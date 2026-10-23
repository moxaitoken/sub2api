"""Refresh the labelled local playground and serve the main admin UI on localhost.

Only --runtime outside the repository is modified. No production connection,
credential display, schema migration, main binary rebuild or account writes.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import time
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--runtime', type=Path, required=True)
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[4]
    runtime = args.runtime.resolve()
    if runtime == repo or repo in runtime.parents:
        parser.error('runtime must be outside the repository')
    compose = runtime / 'compose.json'
    config = json.loads(compose.read_text(encoding='utf-8'))
    for service in ('portal', 'upstream'):
        labels = config['services'][service].get('labels', {})
        if labels.get('codex.task') != 'sub2api-supplier-local' or Path(labels.get('codex.scope', '')).resolve() != runtime:
            raise RuntimeError('refusing unowned local services')
    # No unbounded host publish or unexpected service/port rewrite.
    upstream = config['services']['upstream']
    if upstream.get('ports') != ['127.0.0.1:8082:8080']:
        raise RuntimeError('expected the original local upstream on port 8082')
    if config['services']['portal'].get('ports') != ['127.0.0.1:5174:8091']:
        raise RuntimeError('expected the original local supplier portal')
    env_path = runtime / 'supplier.env'
    private_env = dict(line.split('=', 1) for line in env_path.read_text(encoding='utf-8').splitlines() if '=' in line and not line.startswith('#'))
    if private_env.get('SUPPLIER_UPSTREAM_URL') != 'http://upstream:8080' or '@postgres:5432/sub2api_supplier?' not in private_env.get('SUPPLIER_DATABASE_URL', ''):
        raise RuntimeError('refusing a non-playground upstream or supplier database')
    with socket.socket() as check:
        check.bind(('127.0.0.1', 8083))
    vite = repo / 'frontend/node_modules/vite/bin/vite.js'
    node = shutil.which('node')
    if not vite.is_file() or not node:
        raise RuntimeError('install main frontend locked dependencies first')
    out = runtime / ('main-admin-' + uuid.uuid4().hex[:8])
    out.mkdir()
    prefix = ['docker', 'compose', '--project-name', 'supplier-local-20261022', '-f', str(compose)]
    records = []

    def save():
        (out / 'VERIFICATION.json').write_text(json.dumps(records, indent=2), encoding='utf-8')

    def run(label, command):
        result = subprocess.run(command, capture_output=True, encoding='utf-8', errors='replace')
        records.append(dict(label=label, command=command, stdout=result.stdout, stderr=result.stderr, exit_status=result.returncode))
        save()
        print(label, result.returncode, flush=True)
        if result.returncode:
            raise RuntimeError(label + ' failed; literal output retained')
        return result

    run('runtime_identity', prefix + ['ps', '--format', 'json'])
    run('build_supplier', ['docker', 'run', '--rm', '--pull=never', '--network', 'none',
        '-v', str(repo / 'extensions/supplier/backend') + ':/app:ro',
        '-v', str(runtime / 'bin') + ':/out', '-v', 'supplier-s1-gomod:/go/pkg/mod',
        '-v', 'supplier-s1-gobuild:/root/.cache/go-build', '-w', '/app',
        '-e', 'CGO_ENABLED=0', '-e', 'GOPROXY=off', '-e', 'GOTOOLCHAIN=local',
        'golang:1.27.2-alpine', 'go', 'build', '-o', '/out/supplier-server.main-admin-next', './cmd/server'])
    shutil.copyfile(compose, out / 'compose.before.json')
    shutil.copyfile(env_path, out / 'supplier.env.before')
    shutil.copyfile(runtime / 'bin/supplier-server', out / 'supplier-server.before')
    process = None
    try:
        run('stop_local_apps', prefix + ['stop', 'portal', 'upstream'])
        upstream['ports'] = ['127.0.0.1:8083:8080']
        compose.write_text(json.dumps(config, indent=2), encoding='utf-8')
        # Preserve private settings without printing or recording their values.
        lines = [line for line in env_path.read_text(encoding='utf-8').splitlines() if not line.startswith('SUPPLIER_ADMIN_ORIGIN=')]
        env_path.write_text('\n'.join(lines) + '\nSUPPLIER_ADMIN_ORIGIN=http://127.0.0.1:8082\n', encoding='utf-8', newline='\n')
        os.replace(runtime / 'bin/supplier-server.main-admin-next', runtime / 'bin/supplier-server')
        run('start_local_apps', prefix + ['up', '-d', '--no-deps', '--force-recreate', 'upstream', 'portal'])
        child_env = os.environ.copy()
        child_env.update(VITE_DEV_PORT='8082', VITE_DEV_PROXY_TARGET='http://127.0.0.1:8083', VITE_DEV_SUPPLIER_TARGET='http://127.0.0.1:5174')
        with (out / 'vite.stdout.log').open('wb') as stdout, (out / 'vite.stderr.log').open('wb') as stderr:
            process = subprocess.Popen([node, str(vite), '--host', '127.0.0.1', '--strictPort'],
                cwd=repo / 'frontend', env=child_env, stdout=stdout, stderr=stderr,
                creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0)
        for attempt in range(45):
            if process.poll() is not None:
                raise RuntimeError('main local frontend exited; see Vite logs')
            try:
                with urllib.request.urlopen('http://127.0.0.1:8082/admin/suppliers', timeout=2) as response:
                    if response.status == 200:
                        break
            except (OSError, TimeoutError):
                time.sleep(1)
        else:
            raise RuntimeError('main frontend did not become ready')
        state = dict(pid=process.pid, origin='http://127.0.0.1:8082', backend='http://127.0.0.1:8083',
            portal='http://127.0.0.1:5174', backup=str(out), source=str(repo),
            supplier_binary_sha256=hashlib.sha256((runtime / 'bin/supplier-server').read_bytes()).hexdigest())
        (runtime / 'main-admin-runtime.json').write_text(json.dumps(state, indent=2), encoding='utf-8')
        records.append(dict(label='local_main_admin_ready', stdout=json.dumps(state), stderr='', exit_status=0))
        save()
        print('Main administrator UI: http://127.0.0.1:8082/admin/suppliers', flush=True)
    except BaseException:
        if process and process.poll() is None:
            process.terminate()
            process.wait(timeout=10)
        run('stop_failed_local_apps', prefix + ['stop', 'portal', 'upstream'])
        shutil.copyfile(out / 'compose.before.json', compose)
        shutil.copyfile(out / 'supplier.env.before', env_path)
        shutil.copyfile(out / 'supplier-server.before', runtime / 'bin/supplier-server')
        run('restore_local_apps', prefix + ['up', '-d', '--no-deps', '--force-recreate', 'upstream', 'portal'])
        raise


if __name__ == '__main__':
    main()
