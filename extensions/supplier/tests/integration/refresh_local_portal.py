"""Build and refresh the explicitly labelled local supplier playground only.

Keeps private configuration and DB backups in --runtime, not source control.
Never accepts a production origin, production database or unlabelled container.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import urllib.error
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--runtime', type=Path, required=True)
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[4]
    runtime = args.runtime.resolve()
    if runtime == repo or repo in runtime.parents:
        parser.error('private --runtime must be outside the repository')
    compose = runtime / 'compose.json'
    config = json.loads(compose.read_text(encoding='utf-8'))
    labels = config['services']['portal']['labels']
    if labels.get('codex.task') != 'sub2api-supplier-local' or labels.get('codex.scope') != str(runtime):
        raise RuntimeError('refusing a runtime not owned by this local playground')
    env_path = runtime / 'supplier.env'
    env = dict(line.split('=', 1) for line in env_path.read_text(encoding='utf-8').splitlines() if '=' in line)
    if env.get('SUPPLIER_UPSTREAM_URL') != 'http://upstream:8080' or '@postgres:5432/sub2api_supplier?' not in env.get('SUPPLIER_DATABASE_URL', ''):
        raise RuntimeError('refusing non-playground upstream or database')
    prefix = ['docker', 'compose', '--project-name', 'supplier-local-20261022', '-f', str(compose)]
    records = []
    output = runtime / ('s2-refresh-' + uuid.uuid4().hex[:8])
    output.mkdir()

    def save():
        (output / 'VERIFICATION.json').write_text(json.dumps(records, indent=2), encoding='utf-8')

    def command(label, command):
        result = subprocess.run(command, capture_output=True, encoding='utf-8', errors='replace')
        records.append(dict(label=label, command=command, stdout=result.stdout, stderr=result.stderr, exit_status=result.returncode))
        save()
        print(f'{label}: exit={result.returncode}', flush=True)
        if result.returncode:
            raise RuntimeError(label + ' failed; see local verification output')
        return result

    # The image is a previously verified local upstream build. Inspect only
    # non-secret identity/labels, never print the container environment.
    identity = command('runtime_identity', prefix + ['ps', '--format', 'json'])
    if 'supplier-local-20261022' not in identity.stdout:
        raise RuntimeError('local playground not running')
    login_file = dict(line.split(': ', 1) for line in (runtime / 'LOGIN.txt').read_text(encoding='utf-8').splitlines() if ': ' in line)
    base = 'http://127.0.0.1:8082'
    token = ''

    def api(method, path, body=None, secret=False):
        headers = {'Content-Type': 'application/json', 'User-Agent': 'SupplierLocalDevelopment/1.0'}
        if token:
            headers['Authorization'] = 'Bearer ' + token
        request = urllib.request.Request(base + path, json.dumps(body).encode() if body is not None else None, headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=15) as response:
                data = json.loads(response.read())
                records.append(dict(method=method, path=path, http_status=response.status,
                                    stdout='[local-only credentials not recorded]' if secret else json.dumps(data), stderr='', exit_status=0))
                save()
                return data['data']
        except urllib.error.HTTPError as error:
            records.append(dict(method=method, path=path, http_status=error.code, stdout='[upstream error body not recorded]', stderr='', exit_status=1))
            save()
            raise RuntimeError('local management request failed: HTTP ' + str(error.code)) from None

    token = api('POST', '/api/v1/auth/login', {'email': login_file['Email'], 'password': login_file['Password']}, True)['access_token']
    compliance = api('GET', '/api/v1/admin/compliance')
    if compliance.get('required'):
        raise RuntimeError('actual administrator must review compliance; no automatic acceptance')
    private_path = runtime / 'runtime.json'
    private = json.loads(private_path.read_text(encoding='utf-8'))
    existing = api('GET', '/api/v1/admin/settings/admin-api-key', secret=True)
    if not existing.get('exists'):
        private['s2_admin_key'] = api('POST', '/api/v1/admin/settings/admin-api-key/regenerate', {}, True)['key']
        private_path.write_text(json.dumps(private), encoding='utf-8')
    elif not private.get('s2_admin_key'):
        raise RuntimeError('existing local admin key not owned by this helper; refusing rotation')

    for suffix, exclusive in [('quarantine', True), ('approved', False)]:
        field = 's2_' + suffix + '_group'
        if field not in private:
            group = api('POST', '/api/v1/admin/groups', {'name': 'Supplier local ' + suffix + ' ' + uuid.uuid4().hex[:6], 'platform': 'anthropic', 'rate_multiplier': 1, 'is_exclusive': exclusive})
            private[field] = group['id']
            if exclusive:
                api('PUT', '/api/v1/admin/groups/' + str(group['id']), {'status': 'inactive'})
            private_path.write_text(json.dumps(private), encoding='utf-8')
    if 's2_identity_key' not in private:
        private['s2_identity_key'] = base64.b64encode(os.urandom(32)).decode()
        private_path.write_text(json.dumps(private), encoding='utf-8')
    shutil.copyfile(env_path, output / 'supplier.env.before')
    env.update(SUPPLIER_ADMIN_API_KEY=private['s2_admin_key'],
               SUPPLIER_QUARANTINE_GROUP_ID=str(private['s2_quarantine_group']),
               SUPPLIER_APPROVED_GROUP_IDS=str(private['s2_approved_group']),
               SUPPLIER_IDENTITY_KEY_BASE64=private['s2_identity_key'], SUPPLIER_IDENTITY_KEY_ID='local-identity-v1')
    env_path.write_text(''.join(k + '=' + v + '\n' for k, v in env.items()), encoding='utf-8', newline='\n')
    backup = subprocess.run(prefix + ['exec', '-T', 'postgres', 'pg_dump', '-U', 'postgres', '-d', 'sub2api_supplier', '-Fc'], capture_output=True)
    if backup.returncode:
        raise RuntimeError('supplier-only backup failed; refusing migration')
    (output / 'supplier-before.dump').write_bytes(backup.stdout)
    records.append(dict(label='supplier_backup', exit_status=0, bytes=len(backup.stdout), sha256=hashlib.sha256(backup.stdout).hexdigest()))
    save()
    command('build_supplier', ['docker', 'run', '--rm', '--pull=never', '--network', 'none',
            '-v', str(repo / 'extensions/supplier/backend') + ':/app:ro', '-v', str(runtime / 'bin') + ':/out',
            '-v', 'supplier-s1-gomod:/go/pkg/mod', '-v', 'supplier-s1-gobuild:/root/.cache/go-build',
            '-w', '/app', '-e', 'CGO_ENABLED=0', '-e', 'GOPROXY=off', '-e', 'GOTOOLCHAIN=local',
            'golang:1.27.2-alpine', 'go', 'build', '-o', '/out/supplier-server.next', './cmd/server'])
    command('stop_portal_only', prefix + ['stop', 'portal'])
    shutil.copyfile(runtime / 'bin/supplier-server', output / 'supplier-server.before')
    os.replace(runtime / 'bin/supplier-server.next', runtime / 'bin/supplier-server')
    shutil.copytree(repo / 'extensions/supplier/frontend/dist', runtime / 'portal', dirs_exist_ok=True)
    command('explicit_supplier_migration', prefix + ['run', '--rm', '--no-deps', 'portal', '-migrate'])
    command('start_portal', prefix + ['up', '-d', '--no-deps', '--force-recreate', 'portal'])
    for _ in range(20):
        try:
            with urllib.request.urlopen('http://127.0.0.1:5174/health', timeout=2) as response:
                if response.status == 200:
                    records.append(dict(label='portal_health', http_status=200, stdout=response.read().decode(), stderr='', exit_status=0))
                    save()
                    print('Local portal refreshed: http://127.0.0.1:5174', flush=True)
                    return
        except (urllib.error.URLError, TimeoutError):
            time.sleep(1)
    raise RuntimeError('portal not ready after refresh; retain backups and inspect local logs')


if __name__ == '__main__':
    main()
