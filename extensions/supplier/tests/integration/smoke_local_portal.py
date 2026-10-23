"""Check the labelled localhost playground without exchanging a Claude code."""
import argparse
import http.cookiejar
import json
from pathlib import Path
import urllib.error
import urllib.parse
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--runtime', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[4]
    runtime, output = args.runtime.resolve(), args.output.resolve()
    if any(p == repo or repo in p.parents for p in (runtime, output)):
        parser.error('private runtime and output must be outside the repository')
    config = json.loads((runtime / 'compose.json').read_text(encoding='utf-8'))
    labels = config['services']['portal']['labels']
    if labels.get('codex.task') != 'sub2api-supplier-local' or labels.get('codex.scope') != str(runtime):
        raise RuntimeError('refusing a non-playground runtime')
    credentials = dict(line.split(': ', 1) for line in (runtime / 'LOGIN.txt').read_text(encoding='utf-8').splitlines() if ': ' in line)
    output.parent.mkdir(parents=True, exist_ok=True)
    if output.exists():
        raise RuntimeError('refusing to overwrite previous evidence')
    records = []
    csrf = ''

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args):
            return None

    client = urllib.request.build_opener(NoRedirect(), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(method, path, expected, body=None, headers=None, private=False):
        request_headers = {'Origin': 'http://127.0.0.1:5174', 'Content-Type': 'application/json',
                           'User-Agent': 'SupplierLocalSmoke/1.0'}
        if csrf:
            request_headers['X-CSRF-Token'] = csrf
        request_headers.update(headers or {})
        req = urllib.request.Request('http://127.0.0.1:5174' + path,
            json.dumps(body).encode() if body is not None else None, request_headers, method=method)
        try:
            response = client.open(req, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read().decode('utf-8')
            status = response.status
        records.append(dict(method=method, path=path, http_status=status,
                            stdout='[private response omitted]' if private else raw,
                            stderr='', exit_status=0 if status == expected else 1))
        output.write_text(json.dumps(records, indent=2), encoding='utf-8')
        if status != expected:
            raise RuntimeError(f'{method} {path}: expected {expected}, got {status}')
        print(f'{method} {path}: HTTP {status}', flush=True)
        return json.loads(raw).get('data') if raw.startswith('{') else None

    call('POST', '/api/supplier/v1/auth/login', 200,
         {'email': credentials['Email'], 'password': credentials['Password']}, private=True)
    me = call('GET', '/api/supplier/v1/me', 200, private=True)
    assert set(me) == {'user', 'supplier', 'csrf_token'}
    csrf = me['csrf_token']
    accounts = call('GET', '/api/supplier/v1/accounts', 200)
    assert isinstance(accounts['items'], list) and accounts['total'] >= 0
    if me['user']['role'] == 'admin':
        call('GET', '/api/supplier/v1/review/accounts', 200)
    authorization = call('POST', '/api/supplier/v1/oauth/sessions', 201,
                         {'name': 'Local authorization contract check'}, private=True)
    assert set(authorization) == {'id', 'authorization_url', 'expires_at'}
    url = urllib.parse.urlsplit(authorization['authorization_url'])
    assert url.scheme == 'https' and url.netloc == 'claude.com' and url.path == '/cai/oauth/authorize'
    call('POST', '/api/supplier/v1/oauth/sessions/' + authorization['id'] + '/complete', 400,
         {'authorization_code': 'fixture#state-that-does-not-match'}, {'Idempotency-Key': str(uuid.uuid4())})
    for path in ('/members', '/ops', '/usage', '/proxies'):
        call('GET', '/api/supplier/v1' + path, 404)
    call('POST', '/api/supplier/v1/auth/logout', 200, {})
    call('GET', '/api/supplier/v1/me', 401)
    print('No Claude authorization code exchanged; no account created.', flush=True)


if __name__ == '__main__':
    main()
