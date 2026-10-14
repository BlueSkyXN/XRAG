"""Shared Playwright harness for the XRAG workbench (the React app served at /).

Direct navigation to the real server is preferred. Some managed browsers block
loopback URLs; then the page is opened on a fixed test origin whose static files
come from internal/server/web and whose /v1, /mcp and /healthz requests are
forwarded unchanged to the real server. Nothing is mocked: every API response,
including the terminal SSE body, is produced by the running XRAG process.
"""
from pathlib import Path
from urllib.parse import urlsplit
import mimetypes, os, urllib.error, urllib.request

ROOT = Path(__file__).resolve().parents[1]
WEB = ROOT / 'internal/server/web/dist'
TEST_ORIGIN = 'http://xrag.test'


def chromium_path():
    explicit = os.environ.get('XRAG_CHROMIUM')
    if explicit:
        return explicit
    return '/usr/bin/chromium' if Path('/usr/bin/chromium').exists() else None


def _static(path):
    name = path.lstrip('/') or 'index.html'
    target = (WEB / name).resolve()
    if WEB.resolve() in target.parents and target.is_file():
        return target
    # 页面路由回落到 index.html；带扩展名或 assets/ 下的缺失文件保持 404
    if name.startswith('assets/') or '.' in Path(name).name:
        return None
    return WEB / 'index.html'


def _forward(base, route, request):
    parts = urlsplit(request.url)
    path = parts.path + (f'?{parts.query}' if parts.query else '')
    if not (path.startswith('/v1/') or path.startswith('/healthz') or path.startswith('/mcp')):
        target = _static(parts.path)
        if target is None:
            route.fulfill(status=404, body=b'not found')
            return
        kind = mimetypes.guess_type(target.name)[0] or 'application/octet-stream'
        route.fulfill(status=200, headers={'content-type': kind}, body=target.read_bytes())
        return
    headers = {k: v for k, v in request.headers.items() if k.lower() in ('content-type', 'cookie', 'accept')}
    if request.method not in ('GET', 'HEAD'):
        # 服务端按 Origin 与 Host 是否一致拒绝跨源写入；转发时使用真实服务的源
        headers['Origin'] = base
    req = urllib.request.Request(base + path, data=request.post_data_buffer, method=request.method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            status, body, got = r.status, r.read(), r.headers
    except urllib.error.HTTPError as e:
        status, body, got = e.code, e.read(), e.headers
    out = {'content-type': got.get('Content-Type', 'application/json')}
    if got.get('Set-Cookie'):
        out['set-cookie'] = got.get('Set-Cookie')
    route.fulfill(status=status, headers=out, body=body)


def open_workbench(pw, base, width=1440, height=1050):
    """Return (browser, page, origin, transport, errors)."""
    browser = pw.chromium.launch(executable_path=chromium_path(), headless=True, args=['--no-sandbox'])
    errors = []

    def new_page():
        page = browser.new_page(viewport={'width': width, 'height': height})
        page.on('pageerror', lambda e: errors.append(str(e)))
        return page

    page = new_page()
    # XRAG_BROWSER_TRANSPORT=test-origin forces the fallback, so it can be verified on machines without the block
    if os.environ.get('XRAG_BROWSER_TRANSPORT') != 'test-origin':
        try:
            page.goto(base + '/', wait_until='domcontentloaded', timeout=12000)
            return browser, page, base, 'direct URL', errors
        except Exception as e:  # noqa: BLE001 - only the managed-browser block is expected here
            if 'BLOCKED_BY_ADMINISTRATOR' not in str(e):
                raise
    page.close()
    page = new_page()
    page.route(TEST_ORIGIN + '/**', lambda route, request: _forward(base, route, request))
    page.goto(TEST_ORIGIN + '/', wait_until='domcontentloaded', timeout=12000)
    return browser, page, TEST_ORIGIN, 'test origin; built files from disk, API forwarded to the real server', errors


def login_with_key(page, key):
    page.locator('#key-form input[type=password]').fill(key)
    page.locator('#key-form button[type=submit]').click()
    page.get_by_role('navigation', name='工作台').first.wait_for(state='attached', timeout=15000)


def go(page, origin, path):
    page.goto(origin + path, wait_until='domcontentloaded')


def logout(page):
    page.get_by_role('button', name='账号').click()
    page.get_by_role('menuitem', name='退出登录').click()
    page.locator('#key-form, #password-form').first.wait_for(state='visible', timeout=10000)
