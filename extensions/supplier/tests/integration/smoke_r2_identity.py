"""Verify R2 D1 against the explicitly labelled localhost playground only."""
import argparse
import http.cookiejar
import json
from pathlib import Path
import secrets
import urllib.error
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--runtime', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    runtime, output = args.runtime.resolve(), args.output.resolve()
    repo = Path(__file__).resolve().parents[4]
    if any(p == repo or repo in p.parents for p in (runtime, output)):
        parser.error('private runtime/output must be outside the repository')
    labels = json.loads((runtime / 'compose.json').read_text(encoding='utf-8'))['services']['portal']['labels']
    if labels.get('codex.task') != 'sub2api-supplier-local' or labels.get('codex.scope') != str(runtime):
        raise RuntimeError('refusing an unowned local playground')
    if output.exists():
        raise RuntimeError('refusing to overwrite evidence')
    records = []
    admin_csrf, supplier_csrf = '', ''
    login = dict(line.split(': ', 1) for line in (runtime / 'LOGIN.txt').read_text(encoding='utf-8').splitlines() if ': ' in line)

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args):
            return None

    def client():
        return urllib.request.build_opener(NoRedirect(), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    admin, supplier = client(), client()

    def call(client, method, path, expected, body=None, csrf='', key='', version=None, private=False):
        headers = {'Origin': 'http://127.0.0.1:5174', 'Content-Type': 'application/json', 'User-Agent': 'SupplierR2LocalSmoke/1.0'}
        if csrf:
            headers['X-CSRF-Token'] = csrf
        if key:
            headers['Idempotency-Key'] = key
        if version is not None:
            headers['If-Match'] = str(version)
        request = urllib.request.Request('http://127.0.0.1:5174' + path, json.dumps(body).encode() if body is not None else None, headers, method=method)
        try:
            response = client.open(request, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read().decode()
            status = response.status
        records.append(dict(method=method, path=path, http_status=status, stdout='[private response omitted]' if private else raw, stderr='', exit_status=0 if status == expected else 1))
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(records, indent=2), encoding='utf-8')
        print(f'{method} {path}: HTTP {status}', flush=True)
        if status != expected:
            raise RuntimeError(f'expected {expected}, got {status}; see evidence')
        return json.loads(raw).get('data')

    a = '/api/supplier/admin/v1'
    s = '/api/supplier/v1'
    call(admin, 'POST', a + '/auth/login', 200, {'email': login['Email'], 'password': login['Password']}, private=True)
    profile = call(admin, 'GET', a + '/me', 200, private=True)
    assert profile['principal']['kind'] == 'admin' and 'supplier' not in profile
    admin_csrf = profile['csrf_token']
    name = 'Local R2 test supplier'
    account = 'vendor_' + uuid.uuid4().hex[:8]
    password = secrets.token_urlsafe(18) + 'A1'
    payload = {'name': name, 'login_account': account, 'initial_password': password}
    key = str(uuid.uuid4())
    row = call(admin, 'POST', a + '/suppliers', 201, payload, admin_csrf, key)
    assert all(row[k] == '0.00' for k in ('unsettled_usd', 'settled_usd', 'total_consumption_usd'))
    replay = call(admin, 'POST', a + '/suppliers', 200, payload, admin_csrf, key)
    assert replay['id'] == row['id']
    call(supplier, 'POST', s + '/auth/login', 200, {'login_account': account, 'password': password}, private=True)
    profile = call(supplier, 'GET', s + '/me', 200, private=True)
    supplier_csrf = profile['csrf_token']
    assert profile['supplier']['id'] == row['id']
    call(supplier, 'GET', a + '/suppliers', 401)
    empty = call(supplier, 'GET', s + '/accounts', 200)
    assert empty['total'] == 0
    target = a + '/suppliers/' + row['id']
    row = call(admin, 'PUT', target + '/status', 200, {'enabled': False}, admin_csrf, str(uuid.uuid4()), row['version'])
    call(supplier, 'GET', s + '/me', 401)
    call(supplier, 'POST', s + '/auth/login', 401, {'login_account': account, 'password': password})
    call(admin, 'GET', target + '/accounts', 200)
    row = call(admin, 'PUT', target + '/status', 200, {'enabled': True}, admin_csrf, str(uuid.uuid4()), row['version'])
    new_password = secrets.token_urlsafe(18) + 'B2'
    row = call(admin, 'POST', target + '/credential-reset', 200, {'new_password': new_password}, admin_csrf, str(uuid.uuid4()), row['version'])
    call(supplier, 'POST', s + '/auth/login', 401, {'login_account': account, 'password': password})
    call(supplier, 'POST', s + '/auth/login', 200, {'login_account': account, 'password': new_password}, private=True)
    profile = call(supplier, 'GET', s + '/me', 200, private=True)
    supplier_csrf = profile['csrf_token']
    call(supplier, 'POST', s + '/auth/logout', 200, {}, supplier_csrf)
    call(supplier, 'GET', s + '/me', 401)
    for path in ('/review/accounts', '/members', '/ops', '/oauth/sessions'):
        call(supplier, 'GET', s + path, 404)
    (runtime / 'R2_LOGIN.txt').write_text('Supplier URL: http://127.0.0.1:5174/\nAdmin URL: http://127.0.0.1:5174/?portal=admin\nLogin account: ' + account + '\nPassword: ' + new_password + '\nAdmin credentials: use LOGIN.txt\n', encoding='utf-8')
    print('R2 local identity lifecycle verified; test credentials saved in private R2_LOGIN.txt. No Claude calls.', flush=True)


if __name__ == '__main__':
    main()
