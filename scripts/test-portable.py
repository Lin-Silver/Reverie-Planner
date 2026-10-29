"""Native platform lifecycle, ownership, portable paths and restart test."""
import contextlib, json, pathlib, shutil, socket, sqlite3, subprocess, sys, tempfile, time, urllib.request
root=pathlib.Path(__file__).resolve().parents[1]
exe='ReveriePlanner.exe' if sys.platform=='win32' else 'ReveriePlanner'
http=urllib.request.build_opener(urllib.request.ProxyHandler({}))
with tempfile.TemporaryDirectory(prefix='reverie portable ') as tmp:
    dest=pathlib.Path(tmp);(dest/'config').mkdir();(dest/'runtime').mkdir()
    shutil.copy(root/'runtime'/exe,dest/'runtime'/exe)
    shutil.copy(root/'config/providers.example.json',dest/'config/providers.example.json')
    shutil.copytree(root/'src/frontend/dist',dest/'src/frontend/dist')
    with socket.socket() as sock: sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
    cfg=json.loads((root/'config/app.json').read_text());cfg['port']=port
    (dest/'config/app.json').write_text(json.dumps(cfg))
    url=f'http://127.0.0.1:{port}'
    def api(path, method='GET', payload=None):
        body = None if payload is None else json.dumps(payload).encode()
        request = urllib.request.Request(url+path, data=body, method=method, headers={'Content-Type':'application/json'})
        with http.open(request) as response:
            data=response.read()
            return json.loads(data) if data else None
    for cycle in range(2):
        proc=subprocess.Popen([str(dest/'runtime'/exe),'--root='+str(dest)],cwd=tempfile.gettempdir())
        try:
            for attempt in range(100):
                if proc.poll() is not None: raise RuntimeError('service exited early')
                try:
                    with http.open(url+'/api/health',timeout=1) as res: assert json.load(res)['status']=='ok'
                    break
                except OSError: time.sleep(.1)
            else: raise RuntimeError('service did not become ready')
            with http.open(url+'/api/workspace') as res:
                workspace=json.load(res);assert pathlib.Path(workspace['root'])==dest;assert workspace['version']=='0.0.1'
            with http.open(url+'/') as res: assert b'<div id="root">' in res.read()
            # An unrelated root's token cannot shut down this service.
            req=urllib.request.Request(url+'/api/shutdown',method='POST',headers={'Authorization':'Bearer wrong-owner'})
            try: http.open(req);raise AssertionError('unauthenticated stop accepted')
            except urllib.error.HTTPError as err: assert err.code in (401,403)
            if cycle == 0:
                saved = api('/api/pages', 'POST', {'title':'Native recovery page','workspace':True,'content':'Original native writing'})
                edited = api('/api/pages','PUT',{**saved,'content':'Newer native writing'})
                api(f"/api/pages?id={edited['id']}&version={edited['version']}",'DELETE')
            else:
                trash=api('/api/page-trash')
                item=next(p for p in trash if p['id']==saved['id'])
                api('/api/page-trash','POST',{'id':item['id'],'version':item['version']})
                recovered=api('/api/pages?id='+saved['id'])
                assert recovered['content']=='Newer native writing' and recovered['version']>edited['version']
                history=api('/api/page-history?pageId='+saved['id'])
                assert any(p['version']==saved['version'] for p in history)
                deleted=api('/api/pages','POST',{'title':'Archive trash page','workspace':True,'content':'Archive retained writing'})
                api(f"/api/pages?id={deleted['id']}&version={deleted['version']}",'DELETE')
            if cycle == 1:
                archive = dest/'native-workspace.zip'
                request = urllib.request.Request(url+'/api/workspace/archive', method='POST')
                with http.open(request) as response: archive.write_bytes(response.read())
            subprocess.run([str(dest/'runtime'/exe),'--stop','--root',str(dest)],check=True)
            assert proc.wait(timeout=15)==0
            with contextlib.closing(sqlite3.connect(dest/'data/planner.db')) as db:
                if cycle==0: db.execute('CREATE TABLE portable_marker (value TEXT)');db.execute("INSERT INTO portable_marker VALUES ('preserved')")
                else: assert db.execute('SELECT value FROM portable_marker').fetchone()==('preserved',)
                db.commit()
        finally:
            if proc.poll() is None: proc.terminate();proc.wait(timeout=15)
    assert not (dest/'runtime/service.json').exists()
    container = dest/'restored container'
    command = [str(dest/'runtime'/exe), '--restore-workspace', str(archive), '--to', str(container)]
    result = subprocess.run(command, check=True, capture_output=True, text=True)
    restored = pathlib.Path(json.loads(result.stdout)['root'])
    assert restored == container/'workspace'
    # Existing destinations are refused without replacing the valid recovered data.
    assert subprocess.run(command, capture_output=True).returncode != 0
    for cycle in range(2):
        proc = subprocess.Popen([str(dest/'runtime'/exe), '--root', str(restored)], cwd=tempfile.gettempdir())
        try:
            for attempt in range(100):
                if proc.poll() is not None: raise RuntimeError('restored service exited early')
                try:
                    with http.open(url+'/api/workspace', timeout=1) as response:
                        assert pathlib.Path(json.load(response)['root']) == restored
                    break
                except OSError: time.sleep(.1)
            else: raise RuntimeError('restored service did not become ready')
            assert api('/api/pages?id='+saved['id'])['content']=='Newer native writing'
            history=api('/api/page-history?pageId='+saved['id'])
            assert any(p['version']==saved['version'] for p in history)
            if cycle == 0:
                item=next(p for p in api('/api/page-trash') if p['id']==deleted['id'])
                api('/api/page-trash','POST',{'id':item['id'],'version':item['version']})
            assert api('/api/pages?id='+deleted['id'])['content']=='Archive retained writing'
            # Restored roots reuse only static assets from the existing package.
            with http.open(url+'/') as response: assert b'<div id="root">' in response.read()
            with http.open(url+'/api/ai/providers') as response: assert json.load(response) == []
            subprocess.run([str(dest/'runtime'/exe), '--stop', '--root', str(restored)], check=True)
            assert proc.wait(timeout=15) == 0
            with contextlib.closing(sqlite3.connect(restored/'data/planner.db')) as db:
                expected = 'preserved' if cycle == 0 else 'restored-only'
                assert db.execute('SELECT value FROM portable_marker').fetchone() == (expected,)
                if cycle == 0: db.execute("UPDATE portable_marker SET value='restored-only'")
                db.commit()
        finally:
            if proc.poll() is None: proc.terminate(); proc.wait(timeout=15)
    with contextlib.closing(sqlite3.connect(dest/'data/planner.db')) as db:
        assert db.execute('SELECT value FROM portable_marker').fetchone() == ('preserved',)
    print('Native workspace archive/CLI restore/restart passed')
print('Native portable lifecycle/restart passed')
