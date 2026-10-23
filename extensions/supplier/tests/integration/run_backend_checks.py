"""Run supplier checks with cached tools and a disposable private PostgreSQL.

Only test logs are written outside the repository. No production configuration,
SSH credentials, browser session or Claude account is read.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import time
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[4]
    output = args.output.resolve()
    if output == repo or repo in output.parents:
        parser.error('--output must be outside the repository')
    output.mkdir(parents=True, exist_ok=False)
    backend = repo / 'extensions/supplier/backend'
    run_id = uuid.uuid4().hex
    name = 'supplier-s2-test-' + run_id[:12]
    records = []
    container = None

    def run(label, command, check=True):
        result = subprocess.run(command, capture_output=True, encoding='utf-8', errors='replace')
        record = dict(label=label, command=command, stdout=result.stdout,
                      stderr=result.stderr, exit_status=result.returncode)
        records.append(record)
        (output / (label + '.json')).write_text(json.dumps(record, indent=2), encoding='utf-8')
        (output / 'VERIFICATION.json').write_text(json.dumps(records, indent=2), encoding='utf-8')
        print(f'{label}: exit={result.returncode}', flush=True)
        if check and result.returncode:
            raise RuntimeError(label + ' failed; see its literal output JSON')
        return result

    runner = ['docker', 'run', '--rm', '--pull=never', '-v', str(backend) + ':/app:ro',
              '-v', 'supplier-s1-gomod:/go/pkg/mod', '-v', 'supplier-s1-gobuild:/root/.cache/go-build',
              '-w', '/app', '-e', 'GOTOOLCHAIN=local', '-e', 'GOPROXY=off', '-e', 'GOSUMDB=off']
    try:
        unit = run('unit', runner + ['--network', 'none', 'golang:1.27.2-alpine',
                                    'go', 'test', '-count=1', '-json', './...'])
        run('vet', runner + ['--network', 'none', 'golang:1.27.2-alpine', 'go', 'vet', './...'])
        run('build', runner + ['--network', 'none', 'golang:1.27.2-alpine', 'go', 'build', '-o', '/tmp/supplier-server', './cmd/server'])
        created = run('create_pg', ['docker', 'run', '-d', '--pull=never', '--name', name,
                                  '--label', 'codex.task=supplier-s2-test', '--label', 'codex.run=' + run_id,
                                  '--network', 'none', '--tmpfs', '/var/lib/postgresql:rw',
                                  '-e', 'POSTGRES_HOST_AUTH_METHOD=trust', '-e', 'POSTGRES_DB=supplier_repository_test',
                                  'postgres:18-alpine'])
        container = created.stdout.strip()
        for attempt in range(45):
            if run('ready_' + str(attempt), ['docker', 'exec', container, 'pg_isready', '-U', 'postgres', '-d', 'supplier_repository_test'], False).returncode == 0:
                break
            time.sleep(1)
        else:
            raise RuntimeError('private PostgreSQL did not become ready')
        integration = run('postgres', runner + ['--network', 'container:' + container,
                         '-e', 'SUPPLIER_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:5432/supplier_repository_test?sslmode=disable',
                         'golang:1.27.2-alpine', 'go', 'test', '-tags=integration', '-p=1', '-count=1', '-json',
                         './internal/repository', './internal/server', './internal/accounts'])
        events = [json.loads(line) for result in (unit, integration) for line in result.stdout.splitlines() if line.startswith('{')]
        # Go emits package-level skip events for packages with no test files.
        # Only an actual named test skip means verification was omitted.
        if any(event.get('Action') == 'skip' and event.get('Test') for event in events):
            raise RuntimeError('a required test was skipped')
        source_hashes = {str(p.relative_to(backend)): hashlib.sha256(p.read_bytes()).hexdigest()
                         for p in backend.rglob('*') if p.is_file() and p.suffix in ('.go', '.sql')}
        (output / 'RESULT.json').write_text(json.dumps(dict(passed=True, skipped=0,
            pass_events=sum(event.get('Action') == 'pass' for event in events),
            source_hashes=source_hashes), indent=2), encoding='utf-8')
    finally:
        if container:
            inspected = run('inspect_cleanup', ['docker', 'inspect', container])
            data = json.loads(inspected.stdout)[0]
            if data['Config']['Labels'].get('codex.run') != run_id or data['HostConfig']['NetworkMode'] != 'none' or data['HostConfig'].get('PortBindings'):
                raise RuntimeError('refusing to remove a container outside this disposable run')
            run('cleanup', ['docker', 'rm', '-f', container])


if __name__ == '__main__':
    main()
