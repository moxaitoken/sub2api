#!/usr/bin/env python3
"""ECS-side executor for a prepared Sub2API live release.

This file is copied into a release directory by live-release.py.  It never
prints environment values and never performs database downgrade operations.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import time
import urllib.request
from pathlib import Path


COMMAND_LOG: list[dict] = []
MAIN_PG = 'export PGHOST="$DATABASE_HOST" PGPORT="$DATABASE_PORT" PGUSER="$DATABASE_USER" PGPASSWORD="$DATABASE_PASSWORD" PGDATABASE="$DATABASE_DBNAME" PGSSLMODE="$DATABASE_SSLMODE"; exec '


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def record(command: list[str], result: subprocess.CompletedProcess) -> None:
    sensitive = "inspect" in command
    output = result.stdout or ""
    COMMAND_LOG.append({"command": command, "stdout": "[inspect output withheld: may contain runtime credentials]" if sensitive else output, "stderr": result.stderr or "", "exit_status": result.returncode})


def run(command: list[str], *, check: bool = True, stdin=None, stdout=None, cwd=None) -> subprocess.CompletedProcess:
    result = subprocess.run(command, encoding="utf-8", errors="replace", check=False, stdin=stdin, stdout=subprocess.PIPE if stdout is None else stdout, stderr=subprocess.PIPE, cwd=cwd)
    record(command, result)
    if check and result.returncode:
        raise RuntimeError(f"command failed ({result.returncode}): {' '.join(command)}")
    return result


def write_json(path: Path, value: object) -> None:
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8", newline="")


def env_replace(path: Path, values: dict[str, str]) -> bytes:
    original = path.read_bytes()
    lines = original.decode("utf-8").splitlines()
    seen: set[str] = set()
    output: list[str] = []
    for line in lines:
        key = line.split("=", 1)[0] if "=" in line and not line.lstrip().startswith("#") else ""
        if key in values:
            output.append(f"{key}={values[key]}")
            seen.add(key)
        else:
            output.append(line)
    for key, value in values.items():
        if key not in seen:
            output.append(f"{key}={value}")
    return ("\n".join(output) + "\n").encode("utf-8")


def compose_args(root: Path, release: Path, env: Path, components: set[str]) -> list[str]:
    command = ["docker", "compose", "-p", "sub2api", "--env-file", str(env), "-f", str(release / "compose.production.yml")]
    command += ["-f", str(release / "compose.supplier.yml")]
    return command


def service_names(components: set[str]) -> list[str]:
    return [name for component in ("main", "supplier") if component in components for name in (("sub2api",) if component == "main" else ("supplier",))]


def wait_healthy(name: str, image: str, timeout: int = 180) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        info = json.loads(run(["docker", "inspect", name]).stdout)[0]
        health = info.get("State", {}).get("Health", {}).get("Status")
        if health == "healthy":
            if info.get("Config", {}).get("Image") != image:
                raise RuntimeError(f"{name} resolved to an unexpected image")
            if info.get("RestartCount") != 0:
                raise RuntimeError(f"{name} restarted during activation")
            ports = info.get("NetworkSettings", {}).get("Ports", {})
            if any(binding.get("HostIp") != "127.0.0.1" for values in ports.values() for binding in (values or [])):
                raise RuntimeError(f"{name} has a non-loopback published port")
            return
        time.sleep(2)
    raise RuntimeError(f"{name} did not become healthy")


def local_health(url: str) -> int:
    with urllib.request.urlopen(url, timeout=10) as response:
        return response.status


def schema_snapshot(root: Path) -> dict:
    main = run(["docker", "exec", "sub2api", "sh", "-c", MAIN_PG + "psql -X -A -t -v ON_ERROR_STOP=1 -c 'SELECT count(*) FROM schema_migrations'"]).stdout.strip()
    supplier = run(["docker", "run", "--rm", "--pull=never", "--network", "host", "--env-file", str(root / "supplier-migration.env"), "postgres:18-alpine", "sh", "-c", 'exec psql --dbname="$SUPPLIER_DATABASE_URL" -X -A -t -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM supplier.schema_migrations"']).stdout.strip()
    return {"main": int(main), "supplier": int(supplier)}


def verify_invariants(root: Path, before: Path) -> None:
    for name in ("supplier.env", "supplier-migration.env"):
        if digest(root / name) != digest(before / name):
            raise RuntimeError("unexpected configuration drift: " + name)
    if digest(Path("/etc/caddy/Caddyfile")) != digest(before / "Caddyfile"):
        raise RuntimeError("Caddy configuration changed during release")


def validate_preflight(root: Path, release: Path, manifest: dict) -> None:
    if root.resolve() != Path("/opt/sub2api") or release.resolve() != root / "releases" / manifest["release_id"]:
        raise RuntimeError("release path does not match the fixed live destination")
    if manifest["target"]["host"] != "root@47.236.9.72" or manifest["environment"] != "live":
        raise RuntimeError("unexpected destination")
    for name in ("production.env", "supplier.env", "supplier-migration.env", "compose.production.yml", "compose.supplier.yml"):
        if not (root / name).is_file():
            raise RuntimeError("required live file missing: " + name)
    # Do not silently replace live networking, volume or environment contracts.
    for name in ("compose.production.yml", "compose.supplier.yml"):
        if (root / name).read_text().replace("\r\n", "\n") != (release / name).read_text().replace("\r\n", "\n"):
            raise RuntimeError("live Compose drift requires review: " + name)
    if shutil.disk_usage(root).free < 3 * (release / "images.tar").stat().st_size + 1024 ** 3:
        raise RuntimeError("insufficient disk for images and backups")
    run(["sha256sum", "-c", "images.sha256"], cwd=release)


def backup(root: Path, release: Path, components: set[str]) -> Path:
    before = release / "before"
    before.mkdir(mode=0o700, exist_ok=False)
    for name in ("production.env", "supplier.env", "supplier-migration.env"):
        source = root / name
        if source.is_file():
            target = before / name
            shutil.copyfile(source, target)
            target.chmod(0o600)
    for name in ("compose.production.yml", "compose.supplier.yml"):
        source = root / name
        if source.is_file():
            shutil.copyfile(source, before / name)
    caddy = Path("/etc/caddy/Caddyfile")
    if caddy.is_file():
        shutil.copyfile(caddy, before / "Caddyfile")
    containers = {}
    for name in ("sub2api", "sub2api-supplier"):
        inspected = run(["docker", "inspect", name])
        if inspected.returncode == 0:
            containers[name] = json.loads(inspected.stdout)[0]
            if containers[name].get("State", {}).get("Health", {}).get("Status") != "healthy":
                raise RuntimeError(f"baseline container {name} is not healthy")
    write_json(before / "containers.json", {name: {"image": value.get("Config", {}).get("Image"), "restart_count": value.get("RestartCount")} for name, value in containers.items()})
    main_dump = before / "main.dump"
    with main_dump.open("wb") as stream:
        run(["docker", "exec", "sub2api", "sh", "-c", MAIN_PG + "pg_dump -Fc --no-owner --no-acl"], stdout=stream)
    main_dump.chmod(0o600)
    if "supplier" in components and (root / "supplier-migration.env").is_file():
        supplier_dump = before / "supplier.dump"
        with supplier_dump.open("wb") as stream:
            run(["docker", "run", "--rm", "--network", "host", "--pull=never", "--env-file", str(root / "supplier-migration.env"), "postgres:18-alpine", "sh", "-c", "pg_dump --dbname=\"$SUPPLIER_DATABASE_URL\" -Fc --no-owner --no-acl"], stdout=stream)
        supplier_dump.chmod(0o600)
    with (before / "app-data.tar.gz").open("wb") as stream:
        run(["docker", "exec", "sub2api", "tar", "czf", "-", "-C", "/app/data", "."], stdout=stream)
    (before / "app-data.tar.gz").chmod(0o600)
    write_json(before / "hashes.json", {str(path.name): digest(path) for path in before.iterdir() if path.is_file()})
    return before


def restore_check(root: Path, release: Path, before: Path, components: set[str]) -> None:
    if not (before / "main.dump").is_file():
        raise RuntimeError("main database dump is missing")
    container = run(["docker", "run", "-d", "--pull=never", "--label", "sub2api.live.restore=" + release.name, "--network", "none", "--tmpfs", "/var/lib/postgresql:rw", "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "postgres:18-alpine"]).stdout.strip()
    try:
        for _ in range(60):
            if run(["docker", "exec", container, "pg_isready", "-U", "postgres"], check=False).returncode == 0:
                break
            time.sleep(1)
        else:
            raise RuntimeError("isolated PostgreSQL did not start")
        run(["docker", "exec", container, "createdb", "-U", "postgres", "main_restore"])
        with (before / "main.dump").open("rb") as stream:
            run(["docker", "exec", "-i", container, "pg_restore", "--exit-on-error", "--no-owner", "--no-acl", "-U", "postgres", "-d", "main_restore"], stdin=stream)
        main_count = run(["docker", "exec", container, "psql", "-X", "-A", "-t", "-U", "postgres", "-d", "main_restore", "-c", "SELECT count(*) FROM schema_migrations"]).stdout.strip()
        expected = json.loads((before / "schema.json").read_text())
        if int(main_count) != expected["main"]:
            raise RuntimeError("restored main schema does not match baseline")
        if "supplier" in components and (before / "supplier.dump").is_file():
            run(["docker", "exec", container, "createdb", "-U", "postgres", "supplier_restore"])
            with (before / "supplier.dump").open("rb") as stream:
                run(["docker", "exec", "-i", container, "pg_restore", "--exit-on-error", "--no-owner", "--no-acl", "-U", "postgres", "-d", "supplier_restore"], stdin=stream)
            supplier_count = run(["docker", "exec", container, "psql", "-X", "-A", "-t", "-U", "postgres", "-d", "supplier_restore", "-c", "SELECT count(*) FROM supplier.schema_migrations"]).stdout.strip()
            if int(supplier_count) != expected["supplier"]:
                raise RuntimeError("restored supplier schema does not match baseline")
        write_json(release / "restore-verification.json", {"status": "verified", "network": "none", "databases": ["main_restore"] + (["supplier_restore"] if (before / "supplier.dump").is_file() else [])})
    finally:
        info = json.loads(run(["docker", "inspect", container]).stdout)[0]
        if info["Config"]["Labels"].get("sub2api.live.restore") != release.name or info["HostConfig"]["NetworkMode"] != "none":
            raise RuntimeError("refusing cleanup outside the isolated restore container")
        run(["docker", "rm", "-f", container], check=False)


def activate(root: Path, release: Path, manifest: dict, before: Path, allow_migrations: bool) -> None:
    components = {item["name"] for item in manifest["components"]}
    tags = manifest["deployment"]["image_tags"]
    run(["sha256sum", "-c", str(release / "images.sha256")], cwd=release)
    run(["docker", "load", "-i", str(release / "images.tar")])
    for name, tag in tags.items():
        info = json.loads(run(["docker", "image", "inspect", tag]).stdout)[0]
        if not info.get("Config", {}).get("Labels", {}).get("org.opencontainers.image.revision") == manifest["source"]["commit"]:
            raise RuntimeError(f"image {tag} does not carry the requested source revision")
    candidate = release / "production.env"
    variables = {"main": "SUB2API_IMAGE", "supplier": "SUPPLIER_IMAGE"}
    candidate.write_bytes(env_replace(root / "production.env", {variables[key]: value for key, value in tags.items()}))
    candidate.chmod(0o600)
    compose = compose_args(root, release, candidate, components)
    run(compose + ["config", "--quiet"])
    if allow_migrations:
        if "supplier" in components:
            run(["docker", "run", "--rm", "--network", "host", "--pull=never", "--env-file", str(root / "supplier-migration.env"), tags["supplier"], "-migrate"])
    pending = root / "production.env.live-next"
    shutil.copyfile(candidate, pending)
    pending.chmod(0o600)
    os.replace(pending, root / "production.env")
    run(compose_args(root, release, root / "production.env", components) + ["up", "-d", "--no-deps", *service_names(components)])
    if "main" in components:
        wait_healthy("sub2api", tags["main"])
    if "supplier" in components:
        wait_healthy("sub2api-supplier", tags["supplier"])
    for url in ("http://127.0.0.1:8080/health", "http://127.0.0.1:8091/health"):
        if url.endswith("8091/health") and "supplier" not in components:
            continue
        if local_health(url) != 200:
            raise RuntimeError(f"local health failed: {url}")


def rollback_now(root: Path, release: Path, manifest: dict) -> None:
    before = release / "before"
    previous = before / "production.env"
    if not previous.is_file():
        raise RuntimeError("rollback environment backup is missing")
    if schema_snapshot(root) != json.loads((before / "schema.json").read_text()):
        raise RuntimeError("schema drift: refusing unsafe image-only rollback")
    pending = root / "production.env.rollback"
    shutil.copyfile(previous, pending)
    pending.chmod(0o600)
    os.replace(pending, root / "production.env")
    components = {item["name"] for item in manifest["components"]}
    run(compose_args(root, before, root / "production.env", components) + ["up", "-d", "--no-deps", *service_names(components)])
    old = json.loads((before / "containers.json").read_text(encoding="utf-8"))
    for name, info in old.items():
        wait_healthy(name, info["image"])


def execute(args: argparse.Namespace) -> int:
    release = Path(args.release_dir)
    root = Path(args.remote_root)
    manifest = json.loads((release / "release-manifest.json").read_text(encoding="utf-8"))
    components = {item["name"] for item in manifest["components"]}
    status = {"release_id": manifest["release_id"], "status": "failed", "rollback": "not_attempted"}
    try:
        validate_preflight(root, release, manifest)
        before = backup(root, release, components)
        baseline = schema_snapshot(root)
        write_json(before / "schema.json", baseline)
        restore_check(root, release, before, components)
        if args.allow_migrations:
            raise RuntimeError("this release has no declared schema migration; refusing implicit migration")
        for name, expected in manifest["deployment"]["expected_schema_versions"].items():
            if baseline[name] != expected:
                raise RuntimeError("schema drift detected for " + name)
        (release / "activation-started").write_text("image-only; schema unchanged\n")
        activate(root, release, manifest, before, args.allow_migrations)
        if schema_snapshot(root) != baseline:
            raise RuntimeError("schema changed unexpectedly during image-only release")
        verify_invariants(root, before)
        status.update(status="activated", rollback="available")
        write_json(release / "release-status.json", status)
        return 0
    except BaseException as error:
        status["error"] = str(error)
        try:
            if (release / "activation-started").exists():
                rollback_now(root, release, manifest)
                status["rollback"] = "completed"
            else:
                status["rollback"] = "not_needed_before_activation"
        except BaseException as rollback_error:
            status["rollback"] = "failed"
            status["rollback_error"] = str(rollback_error)
        write_json(release / "release-status.json", status)
        return 2
    finally:
        (release / "command-log.jsonl").write_text("\n".join(json.dumps(item, ensure_ascii=False) for item in COMMAND_LOG) + "\n", encoding="utf-8", newline="")


def rollback_only(args: argparse.Namespace) -> int:
    release = Path(args.release_dir)
    root = Path(args.remote_root)
    manifest = json.loads((release / "release-manifest.json").read_text(encoding="utf-8"))
    try:
        rollback_now(root, release, manifest)
        write_json(release / "release-status.json", {"release_id": manifest["release_id"], "status": "rolled_back", "rollback": "completed"})
        return 0
    finally:
        (release / "command-log.jsonl").write_text("\n".join(json.dumps(item, ensure_ascii=False) for item in COMMAND_LOG) + "\n", encoding="utf-8", newline="")


def main() -> int:
    import fcntl  # ECS/Linux only; helpers remain importable for Windows tests.
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--release-dir", required=True)
    parser.add_argument("--remote-root", default="/opt/sub2api")
    parser.add_argument("--allow-migrations", action="store_true")
    parser.add_argument("--rollback-only", action="store_true")
    args = parser.parse_args()
    os.umask(0o077)
    with (Path(args.remote_root) / ".live-release.lock").open("a") as lock:
        fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        return rollback_only(args) if args.rollback_only else execute(args)


if __name__ == "__main__":
    raise SystemExit(main())
