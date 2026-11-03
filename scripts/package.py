"""Package only redistributable files; never include user data or credentials.

Archives are deterministic for the same inputs: entries are sorted, all ZIP
timestamps are fixed to 1980-01-01, all tar mtimes/owners are zeroed and the
gzip header mtime is zero. BUILD-MANIFEST.txt records the version, target,
source commit and the SHA-256 of every payload file.
"""
import argparse, gzip, hashlib, io, os, pathlib, subprocess, tarfile, zipfile

VERSION = "0.0.1"
PREFIX = "Reverie-Planner-" + VERSION + "-"
EXECUTABLE = {"runtime/ReveriePlanner.exe", "runtime/ReveriePlanner",
              "start.sh", "stop.sh", "start.command", "stop.command"}


def source_commit(explicit):
    if explicit:
        return explicit
    if os.environ.get("GITHUB_SHA"):
        return os.environ["GITHUB_SHA"]
    try:
        return subprocess.run(["git", "rev-parse", "HEAD"], capture_output=True,
                              text=True, check=True).stdout.strip()
    except Exception:
        return "unknown"


def collect(root, target):
    windows = target.startswith("windows")
    paths = ["config/app.json", "config/providers.example.json", "README.md", "docs/PORTABLE.md"]
    paths += (["runtime/ReveriePlanner.exe", "start.bat", "stop.bat", "scripts/start.ps1", "scripts/stop.ps1"]
              if windows else ["runtime/ReveriePlanner", "start.sh", "stop.sh"])
    if target.startswith("darwin"):
        paths += ["start.command", "stop.command"]
    paths += [str(f.relative_to(root)).replace(os.sep, "/")
              for f in (root / "src/frontend/dist").rglob("*") if f.is_file()]
    for name in paths:
        if not (root / name).is_file():
            raise SystemExit("Missing package input: " + name)
    return sorted(set(paths))


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def build_manifest(root, target, commit, names):
    lines = ["Reverie Planner portable package",
             "version: " + VERSION,
             "target: " + target,
             "source-commit: " + commit,
             "files: " + str(len(names)), ""]
    for name in names:
        lines.append(sha256((root / name).read_bytes()) + "  " + name)
    return ("\n".join(lines) + "\n").encode()


def entry_mode(name):
    return 0o755 if name in EXECUTABLE else 0o644


def write_zip(path, prefix, root, names, manifest):
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as archive:
        entries = [(prefix + "/BUILD-MANIFEST.txt", manifest)]
        entries += [(prefix + "/" + name, (root / name).read_bytes()) for name in names]
        for arcname, data in sorted(entries):
            name = arcname[len(prefix) + 1:]
            info = zipfile.ZipInfo(arcname, date_time=(1980, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.create_system = 3
            info.external_attr = entry_mode(name) << 16
            archive.writestr(info, data)


def write_tar_gz(path, prefix, root, names, manifest):
    with open(path, "wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w") as archive:
                entries = [(prefix + "/BUILD-MANIFEST.txt", manifest)]
                entries += [(prefix + "/" + name, (root / name).read_bytes()) for name in names]
                for arcname, data in sorted(entries):
                    name = arcname[len(prefix) + 1:]
                    info = tarfile.TarInfo(arcname)
                    info.size = len(data)
                    info.mtime = 0
                    info.mode = entry_mode(name)
                    info.uid = info.gid = 0
                    info.uname = info.gname = ""
                    archive.addfile(info, io.BytesIO(data))


parser = argparse.ArgumentParser()
parser.add_argument("--target", required=True)
parser.add_argument("--source-commit", default="")
a = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
windows = a.target.startswith("windows")
names = collect(root, a.target)
manifest = build_manifest(root, a.target, source_commit(a.source_commit), names)
out = root / "release"
out.mkdir(exist_ok=True)
prefix = PREFIX + a.target
archive = out / (prefix + (".zip" if windows else ".tar.gz"))
if windows:
    write_zip(archive, prefix, root, names, manifest)
else:
    write_tar_gz(archive, prefix, root, names, manifest)
print(archive)
