"""Native platform lifecycle, ownership, portable paths and restart test."""
import contextlib, io, zipfile, json, pathlib, shutil, socket, sqlite3, subprocess, sys, tempfile, time, urllib.request
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
    def restore(kind, item):
        return api('/api/db/trash','POST',{'kind':kind,'id':item['id']})
    def reject_restore(kind, item):
        try: restore(kind,item); raise AssertionError('missing dependency was restored')
        except urllib.error.HTTPError as err: assert err.code == 409
    def check_record():
        record=next(r for r in api('/api/db/records?databaseId='+database['id']) if r['id']==native_record['id'])
        assert record['values'][amount['id']] == 11 and record['values'][formula['id']] == 22
        assert record['values'][self_relation['id']] == [native_record['id']]
        assert any(v['id']==view['id'] and v['config']==view['config'] for v in api('/api/db/views?databaseId='+database['id']))
    # Synthetic text-only import travels through restart, archive and CLI restore.
    import_zip=io.BytesIO()
    with zipfile.ZipFile(import_zip,'w',zipfile.ZIP_DEFLATED) as package:
        package.writestr('Native import.md','# Native import\n\nOffline source.')
    import_zip=import_zip.getvalue()
    def import_package():
        request=urllib.request.Request(url+'/api/import/markdown-preview',data=import_zip,method='POST')
        with http.open(request) as response: preview=json.load(response)
        boundary='reverie-native-import-boundary'
        fields=[('digest',preview['digest'].encode()),('operationKey',b'native_import_operation_123456'),('blocks',json.dumps([{'path':'Native import.md','content':[{'type':'paragraph','content':[{'type':'text','text':'Native imported writing','styles':{}}]}]}]).encode())]
        body=b''
        for name,value in fields:
            body+=f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n'.encode()+value+b'\r\n'
        body+=f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="pages.zip"\r\nContent-Type: application/zip\r\n\r\n'.encode()+import_zip+f'\r\n--{boundary}--\r\n'.encode()
        request=urllib.request.Request(url+'/api/import/markdown',data=body,method='POST',headers={'Content-Type':'multipart/form-data; boundary='+boundary})
        with http.open(request) as response: return json.load(response)
    def check_import():
        before=len(api('/api/pages'))
        assert import_package()==imported
        assert len(api('/api/pages'))==before
        document=api('/api/pages?id='+imported['pages']['Native import.md'])
        assert document['parentId']==imported['rootId'] and 'Native imported writing' in document['content']
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
                imported=import_package()
                check_import()
                saved = api('/api/pages', 'POST', {'title':'Native recovery page','workspace':True,'content':'Original native writing'})
                edited = api('/api/pages','PUT',{**saved,'content':'Newer native writing'})
                api(f"/api/pages?id={edited['id']}&version={edited['version']}",'DELETE')
                database=api('/api/databases','POST',{'title':'Synthetic database recovery'})
                amount=api('/api/db/properties','POST',{'databaseId':database['id'],'name':'Amount','type':'number','config':'{}'})
                formula=api('/api/db/properties','POST',{'databaseId':database['id'],'name':'Double','type':'formula','config':json.dumps({'operation':'multiply','args':[{'propertyId':amount['id']},{'number':2}]})})
                self_relation=api('/api/db/properties','POST',{'databaseId':database['id'],'name':'Self reference','type':'relation','config':json.dumps({'targetDatabaseId':database['id']})})
                native_record=api('/api/db/records','POST',{'databaseId':database['id'],'title':'Retained native record','values':{amount['id']:11}})
                native_record=api('/api/db/records','PUT',{**native_record,'values':{self_relation['id']:[native_record['id']]}})
                view=api('/api/db/views','POST',{'databaseId':database['id'],'name':'Retained view','type':'gallery','config':json.dumps({'galleryProperties':[amount['id']]})})
                for path,item in [('/api/db/properties',formula),('/api/db/properties',amount),('/api/db/records',native_record),('/api/db/views',view),('/api/databases',database)]:
                    api(path+'?id='+item['id'],'DELETE')
            else:
                check_import()
                trash=api('/api/page-trash')
                item=next(p for p in trash if p['id']==saved['id'])
                api('/api/page-trash','POST',{'id':item['id'],'version':item['version']})
                recovered=api('/api/pages?id='+saved['id'])
                assert recovered['content']=='Newer native writing' and recovered['version']>edited['version']
                history=api('/api/page-history?pageId='+saved['id'])
                assert any(p['version']==saved['version'] for p in history)
                deleted=api('/api/pages','POST',{'title':'Archive trash page','workspace':True,'content':'Archive retained writing'})
                api(f"/api/pages?id={deleted['id']}&version={deleted['version']}",'DELETE')
                reject_restore('record',native_record)
                restore('database',database)
                reject_restore('property',formula)
                restore('property',amount)
                restore('property',formula)
                restore('view',view)
                restore('record',native_record)
                check_record()
                # Keep dependency-ordered trash in the archive too.
                api('/api/db/properties?id='+formula['id'],'DELETE')
                api('/api/db/properties?id='+amount['id'],'DELETE')
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
            check_import()
            assert api('/api/pages?id='+saved['id'])['content']=='Newer native writing'
            history=api('/api/page-history?pageId='+saved['id'])
            assert any(p['version']==saved['version'] for p in history)
            if cycle == 0:
                item=next(p for p in api('/api/page-trash') if p['id']==deleted['id'])
                api('/api/page-trash','POST',{'id':item['id'],'version':item['version']})
                reject_restore('property',formula)
                restore('property',amount)
                restore('property',formula)
            check_record()
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
