"""Package only redistributable files; never include user data or credentials."""
import argparse, pathlib, tarfile, zipfile
p=argparse.ArgumentParser()
p.add_argument('--target', required=True)
a=p.parse_args()
root=pathlib.Path(__file__).resolve().parents[1]
windows=a.target.startswith('windows')
paths=['config/app.json','config/providers.example.json','README.md','docs/PORTABLE.md']
paths+=['runtime/ReveriePlanner.exe','start.bat','stop.bat','scripts/start.ps1','scripts/stop.ps1'] if windows else ['runtime/ReveriePlanner','start.sh','stop.sh']
if a.target.startswith('darwin'): paths += ['start.command','stop.command']
paths += [str(f.relative_to(root)) for f in (root/'src/frontend/dist').rglob('*') if f.is_file()]
for name in paths:
    if not (root/name).is_file(): raise SystemExit('Missing package input: '+name)
out=root/'release';out.mkdir(exist_ok=True)
prefix='Reverie-Planner-0.0.1-'+a.target
if windows:
    with zipfile.ZipFile(out/(prefix+'.zip'),'w',zipfile.ZIP_DEFLATED) as z:
        for name in paths: z.write(root/name,prefix+'/'+name)
else:
    with tarfile.open(out/(prefix+'.tar.gz'),'w:gz') as z:
        for name in paths: z.add(root/name,arcname=prefix+'/'+name,recursive=False)
print(out/prefix)
