#!/usr/bin/env python3
"""End-to-end check of the workbench served at / against a fresh real XRAG process.

Without --browser it checks the HTTP contract the workbench relies on (page
fallback, +ask routes, files.read line windows, job filters). With --browser it
drives the actual pages in Chromium through scripts/workbench_browser.py.
No model, Qdrant or Neo4j is configured; nothing is mocked.
"""
from pathlib import Path
import argparse, json, socket, subprocess, sys, tempfile, time, urllib.error, urllib.request

ROOT = Path(__file__).resolve().parents[1]
CHECKS = ROOT / 'checks/workbench'
rows = []


def ck(name, ok, detail=None, kind='http'):
    if not ok:
        raise AssertionError((name, detail))
    rows.append({'name': name, 'passed': True, 'kind': kind, 'detail': detail})
    print('PASS ' + name, flush=True)


def free_port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        return s.getsockname()[1]


def main(browser):
    CHECKS.mkdir(parents=True, exist_ok=True)
    server_bin = ROOT / 'bin/xrag-server'
    if not server_bin.exists():
        raise SystemExit('run `make build` first')
    with tempfile.TemporaryDirectory(prefix='xrag-workbench-') as temp:
        data = Path(temp) / 'data'
        base = f'http://127.0.0.1:{free_port()}'
        subprocess.run([str(server_bin), 'init', '--data', str(data), '--examples', str(ROOT / 'examples/docs')], check=True, capture_output=True)
        key = (data / 'admin.key').read_text().strip()
        log = (CHECKS / 'server.log').open('w')
        server = subprocess.Popen([str(server_bin), 'serve', '--data', str(data), '--listen', base.split('://')[1], '--build', 'demo'], stdout=log, stderr=log)

        def api(path, body=None, method=None):
            req = urllib.request.Request(
                base + path,
                data=None if body is None else json.dumps(body, ensure_ascii=False).encode(),
                method=method or ('GET' if body is None else 'POST'),
                headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + key},
            )
            try:
                with urllib.request.urlopen(req, timeout=30) as r:
                    raw = r.read()
                    return r.status, r.headers, raw
            except urllib.error.HTTPError as e:
                return e.code, e.headers, e.read()

        def op(path, body=None, method=None):
            status, _, raw = api(path, body, method)
            if status >= 300:
                raise AssertionError((path, status, raw[:300]))
            return json.loads(raw)

        try:
            for _ in range(200):
                try:
                    if api('/healthz')[0] == 200 and op('/v1/kbs')['items'][0]['files'] >= 7:
                        break
                except (OSError, AssertionError, IndexError, KeyError):
                    pass
                time.sleep(0.1)
            else:
                raise AssertionError('server did not finish the demo build')

            status, _, home = api('/')
            ck('home serves the workbench shell', status == 200 and b'id="root"' in home)
            ck('page paths fall back to the shell', api('/settings/keys')[2] == home)
            ck('missing assets stay 404', api('/assets/missing.js')[0] == 404)

            ask = op('/v1/ask', {'kbs': ['demo'], 'q': '组件B', 'graph': True, 'trace': True})
            routes = {tuple(item.get('routes', [])) for item in ask['items']}
            ck('+ask reports which routes found each result', ('keyword', 'graph') in routes and ('keyword',) in routes, sorted(routes))
            chunk = op('/v1/ask', {'kbs': ['demo'], 'q': 'E203', 'limit': 1})['items'][0]
            window = op('/v1/files/read', {'kbs': ['demo'], 'ref': chunk['ref'], 'context_lines': 3})['data']['window']
            ck('files.read returns cited lines with context', window['cite_start'] == chunk['line_start'] and window['line_start'] < window['cite_start'], window)
            ck('jobs can be filtered by knowledge base and kind', len(op('/v1/jobs?kb=demo&kind=build')['items']) >= 1 and op('/v1/jobs?kind=wiki')['items'] == [])

            if browser:
                browser_checks(base, key, op)
        finally:
            server.terminate()
            try:
                server.wait(timeout=15)
            except subprocess.TimeoutExpired:
                server.kill()
            log.close()
    (CHECKS / 'smoke.json').write_text(json.dumps({'passed': len(rows), 'checks': rows, 'scope': 'fresh real XRAG process, demo documents, no models'}, ensure_ascii=False, indent=2))


def browser_checks(base, key, op):
    sys.dont_write_bytecode = True
    sys.path.insert(0, str(ROOT / 'scripts'))
    from workbench_browser import go, login_with_key, logout, open_workbench
    from playwright.sync_api import sync_playwright

    with sync_playwright() as pw:
        browser, p, origin, transport, errors = open_workbench(pw, base)
        login_with_key(p, key)
        ck('key login opens the workbench', '/search' in p.url, transport, 'browser')

        # 检索：结果、路线、执行过程、原文行窗口、等价调用
        go(p, origin, '/search')
        p.get_by_label('查询', exact=True).fill('E203')
        p.get_by_label('查询', exact=True).press('Enter')
        results = p.get_by_role('region', name='结果')
        results.get_by_role('heading', name='条结果').wait_for(timeout=15000)
        ck('search lists results with their route', results.get_by_text('关键词').count() >= 1, None, 'browser')
        ck('search shows the execution pipeline', p.get_by_role('region', name='执行过程').is_visible(), None, 'browser')
        inspector = p.get_by_role('region', name='检查器')
        inspector.get_by_text('引用 L8–10').wait_for(timeout=10000)
        ck('reader marks the cited lines inside the pinned file', 'E203 表示日志写入队列已满' in inspector.inner_text(), None, 'browser')
        inspector.get_by_role('tab', name='调用').click()
        ck('call tab shows the equivalent CLI', 'xrag +ask E203 --kb demo' in inspector.inner_text(), None, 'browser')
        p.screenshot(path=str(CHECKS / 'search.png'))

        # 只查此文件 → 带范围回到检索
        go(p, origin, '/sources')
        p.locator('tr', has_text='manual/02-errors.md').get_by_role('button', name='只查此文件').click()
        p.get_by_text('--within').first.wait_for(timeout=10000)
        ck('a single file becomes a within scope for search', '/search' in p.url, None, 'browser')

        # 关系
        go(p, origin, '/graph')
        p.get_by_role('button', name='查询', exact=True).click()
        p.get_by_role('img', name='关系图', exact=False).wait_for(timeout=10000)
        ck('graph page draws returned relations', '组件B' in (p.get_by_role('img', name='关系图', exact=False).get_attribute('aria-label') or ''), None, 'browser')

        # 评测：载入样例并运行一次关键词对比
        go(p, origin, '/eval')
        p.get_by_role('button', name='载入合成样例').click()
        p.get_by_text('已载入合成样例').first.wait_for(timeout=10000)
        p.get_by_role('button', name='运行对比').click()
        report = p.get_by_role('button', name='查看报告').first
        for _ in range(100):
            if report.is_enabled():
                break
            p.wait_for_timeout(200)
        report.click()
        p.get_by_text('Hit@K').wait_for(timeout=10000)
        ck('evaluation runs and shows a report', True, None, 'browser')

        # 命令面板
        p.keyboard.press('Control+k')
        palette = p.get_by_role('dialog')
        palette.get_by_text('search keyword').first.wait_for(timeout=5000)
        ck('command palette lists registry actions', palette.get_by_text('files grep').count() >= 1, None, 'browser')
        p.keyboard.press('Escape')

        # 设置：管理员看到全部标签
        go(p, origin, '/settings')
        tabs = p.get_by_role('tab')
        tabs.first.wait_for(timeout=10000)
        ck('admin sees every settings tab', tabs.count() == 5, tabs.all_inner_texts(), 'browser')

        # 移动端不出现整页横向滚动
        p.set_viewport_size({'width': 390, 'height': 844})
        for path in ('/search', '/sources', '/jobs', '/settings'):
            go(p, origin, path)
            p.wait_for_timeout(600)
            ck(f'{path} fits a 390px viewport', p.evaluate('document.documentElement.scrollWidth <= innerWidth'), None, 'browser')
        p.screenshot(path=str(CHECKS / 'mobile.png'))
        p.set_viewport_size({'width': 1440, 'height': 1050})

        logout(p)
        ck('logout returns to the login form', p.locator('#key-form').is_visible(), None, 'browser')

        # Reader：导航只显示查找与系统页面；直接访问维护页面得到角色说明
        reader = op('/v1/keys', {'name': 'smoke reader', 'role': 'reader', 'kbs': ['demo'], 'expires_at': 0})['key']
        login_with_key(p, reader)
        nav = p.get_by_role('navigation', name='工作台').first
        ck('reader navigation hides maintainer pages', nav.get_by_role('link', name='评测').count() == 0 and nav.get_by_role('link', name='知识与 Wiki').count() == 0 and nav.get_by_role('link', name='检索').count() == 1, None, 'browser')
        go(p, origin, '/eval')
        p.get_by_text('评测需要').wait_for(timeout=10000)
        ck('maintainer pages explain the required role', True, None, 'browser')
        go(p, origin, '/sources')
        p.get_by_text('查看和管理来源需要 Maintainer 角色').wait_for(timeout=10000)
        ck('readers can browse published files but not sources', p.get_by_role('heading', name='已发布文件').is_visible(), None, 'browser')
        go(p, origin, '/settings')
        p.get_by_role('tab').first.wait_for(timeout=10000)
        ck('reader sees only the capability settings tab', p.get_by_role('tab').count() == 1, p.get_by_role('tab').all_inner_texts(), 'browser')
        logout(p)
        ck('no JavaScript exceptions', not errors, errors, 'browser')
        browser.close()


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--browser', action='store_true')
    main(parser.parse_args().browser)
