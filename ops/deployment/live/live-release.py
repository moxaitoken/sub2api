#!/usr/bin/env python3
"""Guarded live release orchestrator for the main and supplier services."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import shutil
import subprocess
import sys
import tarfile
import urllib.error
import urllib.request
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Iterable


ROOT = Path(__file__).resolve().parents[3]
DEFAULT_RELEASE_ROOT = Path.home() / ".codex" / "artifacts" / "sub2api-live-releases"
ALLOWED_DIRTY = {Path("ops/deployment/DEPLOYMENT.md")}
REQUIRED_FILES = (
    Path("Dockerfile"),
    Path("ops/deployment/compose.production.yml"),
    Path("extensions/supplier/deploy/compose.production.yml"),
    Path("extensions/supplier/deploy/Dockerfile"),
    Path("ops/deployment/Caddyfile"),
)


@dataclass(frozen=True)
class Component:
    name: str
    image_variable: str
    dockerfile: str
    compose_files: tuple[str, ...]
    health_url: str


COMPONENTS = {
    "main": Component("main", "SUB2API_IMAGE", "Dockerfile", ("ops/deployment/compose.production.yml",), "http://127.0.0.1:8080/health"),
    "supplier": Component("supplier", "SUPPLIER_IMAGE", "extensions/supplier/deploy/Dockerfile", ("ops/deployment/compose.production.yml", "extensions/supplier/deploy/compose.production.yml"), "http://127.0.0.1:8091/health"),
}


def run(command: list[str], *, cwd: Path = ROOT, check: bool = True, stdout=None, stdin=None) -> subprocess.CompletedProcess:
    result = subprocess.run(command, cwd=cwd, encoding="utf-8", errors="replace", check=False, stdin=stdin, stdout=subprocess.PIPE if stdout is None else stdout, stderr=subprocess.PIPE)
    if check and result.returncode:
        message = (result.stderr or result.stdout or "").strip()
        raise RuntimeError(f"command failed ({result.returncode}): {' '.join(command)}\n{message[-4000:]}")
    return result


def git_output(*args: str) -> str:
    return run(["git", *args]).stdout.strip()


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def now_id() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")


def dirty_paths() -> list[Path]:
    output = run(["git", "status", "--porcelain=v1", "--untracked-files=all"]).stdout.rstrip("\r\n")
    result: list[Path] = []
    for line in output.splitlines():
        if not line:
            continue
        raw = line[3:]
        if " -> " in raw:
            raw = raw.split(" -> ", 1)[1]
        result.append(Path(raw))
    return result


def parse_components(value: str) -> list[Component]:
    names = [item.strip().lower() for item in value.split(",") if item.strip()]
    if not names:
        raise ValueError("at least one component is required")
    unknown = sorted(set(names) - set(COMPONENTS))
    if unknown:
        raise ValueError("unknown component(s): " + ", ".join(unknown))
    return [COMPONENTS[name] for name in dict.fromkeys(names)]


def ensure_required_files() -> list[str]:
    missing = [str(path) for path in REQUIRED_FILES if not (ROOT / path).is_file()]
    if missing:
        raise RuntimeError("required deployment file(s) missing: " + ", ".join(missing))
    return [str(path) for path in REQUIRED_FILES]


def build_manifest(components: Iterable[Component], args: argparse.Namespace) -> dict:
    commit = git_output("rev-parse", args.commit)
    branch = git_output("branch", "--show-current")
    dirty = dirty_paths()
    unexpected = sorted(set(dirty) - ALLOWED_DIRTY)
    overlays = {}
    if args.include_release_tooling:
        allowed = {Path(".gitignore"), Path("Dockerfile")}
        allowed.update(path.relative_to(ROOT) for path in (ROOT / "ops/deployment/live").glob("*.*") if path.suffix in {".py", ".ps1", ".md", ".json"})
        unexpected = sorted(set(unexpected) - allowed)
        overlays = {str(path).replace("\\", "/"): sha256(ROOT / path) for path in dirty if path in allowed and path != Path(".gitignore")}
    files = ensure_required_files()
    release_id = args.release_id or f"live-{now_id()}-{commit[:12]}"
    if not re.fullmatch(r"live-[A-Za-z0-9._-]+", release_id) or ".." in release_id:
        raise ValueError("invalid release ID")
    if args.host != "root@47.236.9.72":
        raise ValueError("live destination is fixed to root@47.236.9.72")
    release_root = Path(args.release_root).expanduser().resolve()
    release_dir = release_root / release_id
    components = list(components)
    return {
        "schema_version": 1,
        "release_id": release_id,
        "environment": args.environment,
        "target": {"host": args.host, "project": "sub2api", "remote_root": "/opt/sub2api"},
        "source": {
            "root": str(ROOT),
            "commit": commit,
            "requested_commit": args.commit,
            "branch": branch,
            "dirty_paths": [str(path).replace("\\", "/") for path in dirty],
            "unexpected_dirty_paths": [str(path).replace("\\", "/") for path in unexpected],
        },
        "components": [asdict(component) for component in components],
        "deployment": {
            "release_dir": str(release_dir),
            "remote_release_dir": f"/opt/sub2api/releases/{release_id}",
            "image_tags": {component.name: f"sub2api-{component.name}:live-{release_id.removeprefix('live-')}" for component in components},
            "migration_policy": "declared-only" if args.allow_migrations else "verify-only",
            "expected_schema_versions": {component.name: (296 if component.name == "main" else 13) for component in components},
            "compose_project": "sub2api",
            "never_run": ["docker compose down -v", "database downgrade"],
        },
        "inputs": {"required_files": files, "deployment_files": {path: sha256(ROOT / path) for path in files}},
        "working_copy_overlays": overlays,
        "status": "planned",
    }


def write_json(path: Path, value: object) -> None:
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8", newline="")


def write_plan(manifest: dict) -> Path:
    release_dir = Path(manifest["deployment"]["release_dir"])
    release_dir.mkdir(parents=True, exist_ok=False)
    path = release_dir / "release-manifest.json"
    write_json(path, manifest)
    (release_dir / "PLAN.txt").write_text("\n".join([
        f"release_id: {manifest['release_id']}",
        f"environment: {manifest['environment']}",
        f"commit: {manifest['source']['commit']}",
        f"components: {', '.join(item['name'] for item in manifest['components'])}",
        f"target: {manifest['target']['host']}",
        f"unexpected_dirty_paths: {len(manifest['source']['unexpected_dirty_paths'])}",
        f"manifest: {path}",
    ]) + "\n", encoding="utf-8", newline="")
    return path


def print_plan(manifest: dict, manifest_path: Path) -> None:
    print(json.dumps({
        "release_id": manifest["release_id"],
        "environment": manifest["environment"],
        "commit": manifest["source"]["commit"],
        "branch": manifest["source"]["branch"],
        "components": [item["name"] for item in manifest["components"]],
        "target": manifest["target"]["host"],
        "unexpected_dirty_paths": manifest["source"]["unexpected_dirty_paths"],
        "manifest": str(manifest_path),
        "next": "review the manifest and use --execute; local tooling overlays require --include-release-tooling",
    }, ensure_ascii=False, indent=2))


def prepare_context(manifest: dict) -> Path:
    release_dir = Path(manifest["deployment"]["release_dir"])
    archive = release_dir / "source.tar"
    context = release_dir / "source"
    context.mkdir()
    run(["git", "archive", "--format=tar", "-o", str(archive), manifest["source"]["commit"]])
    with tarfile.open(archive) as source:
        source.extractall(context, filter="data")
    for name, expected in manifest.get("working_copy_overlays", {}).items():
        source = ROOT / name
        if sha256(source) != expected:
            raise RuntimeError("release tooling changed after planning: " + name)
        target = context / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)
    files = {path.relative_to(context).as_posix(): sha256(path) for path in context.rglob("*") if path.is_file()}
    write_json(release_dir / "source-manifest.json", {"commit": manifest["source"]["commit"], "files": files})
    return context


def logged(command: list[str], output_dir: Path, name: str, *, cwd: Path = ROOT) -> None:
    print(name + ": running", flush=True)
    result = subprocess.run(command, cwd=cwd, encoding="utf-8", errors="replace", capture_output=True, check=False)
    write_json(output_dir / f"{name}.json", {"command": command, "stdout": result.stdout, "stderr": result.stderr, "exit_status": result.returncode})
    if result.returncode:
        raise RuntimeError(f"{name} failed with exit {result.returncode}")
    print(name + ": passed", flush=True)


def build_and_test(manifest: dict, context: Path, args: argparse.Namespace) -> None:
    release_dir = Path(manifest["deployment"]["release_dir"])
    checks = release_dir / "checks"
    checks.mkdir()
    if not args.skip_tests:
        integration = context / "extensions/supplier/tests/integration/run_backend_checks.py"
        if integration.is_file():
            logged([sys.executable, str(integration), "--output", str(checks / "supplier-backend")], checks, "supplier-backend-tests", cwd=context)
        else:
            raise RuntimeError("supplier backend integration checker is missing from the source archive")
    tags = manifest["deployment"]["image_tags"]
    for component in manifest["components"]:
        tag = tags[component["name"]]
        command = ["docker", "buildx", "build", "--platform", "linux/amd64", "--load", "--progress", "plain", "--build-arg", f"COMMIT={manifest['source']['commit']}", "--label", f"org.opencontainers.image.revision={manifest['source']['commit']}", "-t", tag]
        if component["name"] == "supplier":
            command += ["-f", component["dockerfile"]]
        command += ["."]
        logged(command, checks, f"{component['name']}-build", cwd=context)
    image_tar = release_dir / "images.tar"
    logged(["docker", "save", "-o", str(image_tar), *tags.values()], checks, "image-save")
    (release_dir / "images.sha256").write_text(f"{sha256(image_tar)}  images.tar\n", encoding="utf-8", newline="")


def stage_local_files(manifest: dict, context: Path) -> None:
    release_dir = Path(manifest["deployment"]["release_dir"])
    shutil.copy2(context / "ops/deployment/compose.production.yml", release_dir / "compose.production.yml")
    shutil.copy2(context / "extensions/supplier/deploy/compose.production.yml", release_dir / "compose.supplier.yml")
    shutil.copy2(context / "ops/deployment/live/remote-release.py", release_dir / "remote-release.py")
    write_json(release_dir / "release-manifest.json", manifest)


def ssh_args() -> list[str]:
    return ["-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes"]


def remote_command(manifest: dict, args: argparse.Namespace) -> None:
    release_dir = Path(manifest["deployment"]["release_dir"])
    host = manifest["target"]["host"]
    remote = manifest["deployment"]["remote_release_dir"]
    remote_root = manifest["target"]["remote_root"]
    run(["ssh", *ssh_args(), host, f"install -d -m 700 {remote}"])
    upload = [release_dir / name for name in ("images.tar", "images.sha256", "compose.production.yml", "compose.supplier.yml", "remote-release.py", "release-manifest.json")]
    run(["scp", *ssh_args(), *(str(path) for path in upload), f"{host}:{remote}/"])
    command = ["python3", f"{remote}/remote-release.py", "--release-dir", remote, "--remote-root", remote_root]
    if args.allow_migrations:
        command.append("--allow-migrations")
    result = run(["ssh", *ssh_args(), host, *command], check=False)
    (release_dir / "remote-release.json").write_text(json.dumps({"command": command, "stdout": result.stdout, "stderr": result.stderr, "exit_status": result.returncode}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8", newline="")
    if result.returncode:
        raise RuntimeError("remote release failed; remote executor attempted image-only rollback")
    for name in ("release-status.json", "command-log.jsonl"):
        run(["scp", *ssh_args(), f"{host}:{remote}/{name}", str(release_dir / name)])


def public_verify(release_dir: Path) -> None:
    checks: list[dict] = []
    for url, expected in (("https://gomoai.ai/health", 200), ("https://gomoai.ai/", 200), ("https://goccmax.com/health", 200), ("https://goccmax.com/", 200), ("https://gomoai.ai/api/supplier/admin/v1/suppliers?page=1&page_size=1", 401), ("https://goccmax.com/api/supplier/v1/me", 401)):
        request = urllib.request.Request(url, headers={"User-Agent": "sub2api-live-release"})
        try:
            with urllib.request.urlopen(request, timeout=40) as response:
                status = response.status
                body = response.read()
        except urllib.error.HTTPError as error:
            status = error.code
            body = error.read()
        checks.append({"url": url, "status": status, "body_sha256": hashlib.sha256(body).hexdigest()})
        if status != expected:
            raise RuntimeError(f"public verification failed for {url}: expected {expected}, got {status}")
    write_json(release_dir / "public-verification.json", {"checks": checks, "status": "verified"})


def execute(manifest: dict, args: argparse.Namespace) -> None:
    unexpected = manifest["source"]["unexpected_dirty_paths"]
    if unexpected:
        raise RuntimeError("unexpected dirty paths block execution: " + ", ".join(unexpected))
    release_dir = Path(manifest["deployment"]["release_dir"])
    context = prepare_context(manifest)
    build_and_test(manifest, context, args)
    stage_local_files(manifest, context)
    remote_command(manifest, args)
    try:
        public_verify(release_dir)
    except Exception:
        recovery = argparse.Namespace(host=manifest["target"]["host"], rollback=manifest["release_id"])
        rollback(recovery)
        raise
    manifest["status"] = "verified"
    write_json(release_dir / "release-manifest.final.json", manifest)


def rollback(args: argparse.Namespace) -> None:
    host = args.host
    remote = f"/opt/sub2api/releases/{args.rollback}"
    run(["ssh", *ssh_args(), host, "test", "-f", f"{remote}/remote-release.py"])
    result = run(["ssh", *ssh_args(), host, "python3", f"{remote}/remote-release.py", "--rollback-only", "--release-dir", remote, "--remote-root", "/opt/sub2api"], check=False)
    if result.returncode:
        raise RuntimeError("remote rollback failed")
    print(result.stdout)


def make_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--environment", choices=["live"], default="live")
    parser.add_argument("--commit", default="HEAD")
    parser.add_argument("--components", default="main,supplier")
    parser.add_argument("--host", default="root@47.236.9.72")
    parser.add_argument("--release-root", default=str(DEFAULT_RELEASE_ROOT))
    parser.add_argument("--release-id")
    parser.add_argument("--allow-migrations", action="store_true")
    parser.add_argument("--include-release-tooling", action="store_true", help="include hash-recorded local deployment-tool changes without committing")
    parser.add_argument("--skip-tests", action="store_true")
    parser.add_argument("--plan", action="store_true")
    parser.add_argument("--execute", action="store_true")
    parser.add_argument("--rollback")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = make_parser().parse_args(argv)
    modes = sum(bool(value) for value in (args.plan, args.execute, args.rollback))
    if modes > 1:
        make_parser().error("--plan, --execute, and --rollback are mutually exclusive")
    if modes == 0:
        args.plan = True
    try:
        if args.rollback:
            rollback(args)
            return 0
        components = parse_components(args.components)
        manifest = build_manifest(components, args)
        if args.execute:
            release_dir = Path(manifest["deployment"]["release_dir"])
            release_dir.mkdir(parents=True, exist_ok=False)
            write_json(release_dir / "release-manifest.json", manifest)
            execute(manifest, args)
            manifest_path = release_dir / "release-manifest.final.json"
        else:
            manifest_path = write_plan(manifest)
        print_plan(manifest, manifest_path)
        return 0
    except (OSError, subprocess.CalledProcessError, RuntimeError, ValueError, tarfile.TarError) as error:
        print(f"live-release: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
