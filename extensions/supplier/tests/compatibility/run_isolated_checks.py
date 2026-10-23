"""Record local prerequisites and test unchanged upstream OAuth session helpers.

No credentials are read, no remote application is contacted, and no database is
started or modified. This is partial evidence, not acceptance of all three gates.
Exit 0: helper tests passed (full gates still pending); 1: helper/check failure.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[4]
    output = args.output.resolve()
    if output == repo or repo in output.parents:
        parser.error("--output must be outside the repository")
    output.mkdir(parents=True, exist_ok=True)
    run_dir = Path(tempfile.mkdtemp(prefix="isolated-", dir=output))
    commands = []

    def run(label, command, cwd=repo, overrides=None, timeout=90):
        env = os.environ.copy()
        env.update(overrides or {})
        try:
            result = subprocess.run(command, cwd=cwd, env=env, capture_output=True,
                                    timeout=timeout, encoding="utf-8", errors="replace")
            record = {"label": label, "command": command, "cwd": str(cwd),
                      "env_overrides": overrides or {}, "exit_code": result.returncode,
                      "stdout": result.stdout, "stderr": result.stderr}
        except (OSError, subprocess.TimeoutExpired) as error:
            record = {"label": label, "command": command, "cwd": str(cwd),
                      "env_overrides": overrides or {}, "exit_code": None,
                      "stdout": "", "stderr": str(error)}
        commands.append(record)
        (run_dir / (label + ".json")).write_text(json.dumps(record, ensure_ascii=False, indent=2), encoding="utf-8")
        print(label + ": exit=" + str(record["exit_code"]))
        return record

    head = run("head", ["git", "rev-parse", "HEAD"])
    docker = run("docker-version", ["docker", "version", "--format", "{{.Server.Version}}"], timeout=25)
    if docker["exit_code"] == 0:
        run("docker-images", ["docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}} {{.ID}}"])
    run("go-version", ["go", "version"])
    isolated_env = {"GOTOOLCHAIN": "local", "GOWORK": "off", "GOPROXY": "off", "GOSUMDB": "off"}
    run("module-test-preflight", ["go", "test", "-count=1", "./internal/pkg/oauth", "-run", "TestSessionStore"],
        cwd=repo / "backend", overrides=isolated_env)

    source_files = [repo / "backend/internal/pkg/oauth/oauth.go",
                    repo / "backend/internal/pkg/oauth/oauth_test.go",
                    Path(__file__).with_name("oauth_sessions_test.go")]
    work = run_dir / "oauth-stdlib-only"
    work.mkdir()
    hashes = []
    for path in source_files:
        before = digest(path)
        shutil.copyfile(path, work / path.name)
        hashes.append({"source": str(path), "sha256": before,
                       "copy_sha256": digest(work / path.name)})
    helper = run("oauth-helper-tests", ["go", "test", "-count=1", "-v", "oauth.go", "oauth_test.go", "oauth_sessions_test.go"],
                 cwd=work, overrides={**isolated_env, "GO111MODULE": "off"}, timeout=120)
    integrity = all(item["sha256"] == item["copy_sha256"] == digest(Path(item["source"])) for item in hashes)
    report = {"source_commit": head["stdout"].strip(), "source_integrity": integrity,
              "sources": hashes, "helper_tests_passed": helper["exit_code"] == 0,
              "all_three_gates_passed": False,
              "gates": {
                  "account_admission": "NOT_RUN: full isolated gateway test required",
                  "claude_oauth": "PARTIAL: memory-session helper only; no token exchange or account creation",
                  "usage_sync": "NOT_RUN: isolated HTTP and PostgreSQL fixtures required"},
              "boundaries": ["No production access", "No secrets read", "No Claude requests",
                             "No automatic legal acceptance", "No Docker resource mutations"],
              "commands": commands}
    report_path = run_dir / "REPORT.json"
    report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print("REPORT=" + str(report_path))
    print("ALL_THREE_GATES_PASSED=false")
    return 0 if helper["exit_code"] == 0 and integrity else 1


if __name__ == "__main__":
    raise SystemExit(main())
