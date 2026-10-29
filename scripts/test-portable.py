"""Native platform lifecycle, ownership, portable paths and restart test."""
import json, pathlib, shutil, socket, sqlite3, subprocess, sys, tempfile, time, urllib.request
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
            subprocess.run([str(dest/'runtime'/exe),'--stop','--root',str(dest)],check=True)
            assert proc.wait(timeout=15)==0
            with sqlite3.connect(dest/'data/planner.db') as db:
                if cycle==0: db.execute('CREATE TABLE portable_marker (value TEXT)');db.execute("INSERT INTO portable_marker VALUES ('preserved')")
                else: assert db.execute('SELECT value FROM portable_marker').fetchone()==('preserved',)
        finally:
            if proc.poll() is None: proc.terminate();proc.wait(timeout=15)
    assert not (dest/'runtime/service.json').exists()
print('Native portable lifecycle/restart passed')
