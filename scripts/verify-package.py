"""Verify a Reverie Planner portable archive before it is published.

Checks archive structure, forbids user data/credentials, validates the embedded
BUILD-MANIFEST.txt SHA-256 values and prints the archive digest.

    python scripts/verify-package.py release/Reverie-Planner-0.0.1-windows-amd64.zip
"""
import argparse, hashlib, json, pathlib, re, sys, tarfile, zipfile

NAME = re.compile(r"Reverie-Planner-(\d+\.\d+\.\d+)-(.+)\.(zip|tar\.gz)$")
FORBIDDEN = ("node_modules/", ".git/", "/data/", "config/providers.json", "runtime/service.json")
FORBIDDEN_SUFFIX = (".db", ".db-wal", ".db-shm", ".log", ".key", ".p12")


def fail(message):
    print(json.dumps({"status": "failed", "reason": message}, indent=2))
    sys.exit(1)


def read_entries(archive, name):
    if name.endswith(".zip"):
        with zipfile.ZipFile(archive) as package:
            names = package.namelist()
            if len(names) != len(set(names)):
                fail("archive contains duplicate entries")
            return {entry: package.read(entry) for entry in names}
    with tarfile.open(archive, "r:gz") as package:
        entries = {}
        for member in package.getmembers():
            if not member.isfile():
                fail("archive contains a non-regular entry: " + member.name)
            if member.name in entries:
                fail("archive contains duplicate entries")
            entries[member.name] = package.extractfile(member).read()
        return entries


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("archive", type=pathlib.Path)
    parser.add_argument("--target", default="")
    args = parser.parse_args()
    match = NAME.match(args.archive.name)
    if not match:
        fail("archive name must be Reverie-Planner-<version>-<target>.zip|tar.gz")
    version, inferred_target, extension = match.groups()
    target = args.target or inferred_target
    if target != inferred_target:
        fail("--target does not match the archive filename")
    prefix = "Reverie-Planner-%s-%s/" % (version, target)
    entries = read_entries(args.archive, args.archive.name)
    names = sorted(entries)

    if any(name.startswith("/") or ".." in pathlib.PurePosixPath(name).parts for name in names):
        fail("archive contains an absolute or traversal path")
    if any(name != prefix + "BUILD-MANIFEST.txt" and not name.startswith(prefix) for name in names):
        fail("archive entry outside the expected prefix: " + prefix)
    if any(marker in name for name in names for marker in FORBIDDEN):
        fail("archive contains a forbidden path")
    if any(name.lower().endswith(FORBIDDEN_SUFFIX) for name in names):
        fail("archive contains a data/credential/log file")

    required = ["config/app.json", "config/providers.example.json", "README.md",
                "docs/PORTABLE.md", "src/frontend/dist/index.html", "BUILD-MANIFEST.txt"]
    if target.startswith("windows"):
        required += ["runtime/ReveriePlanner.exe", "start.bat", "stop.bat",
                     "scripts/start.ps1", "scripts/stop.ps1"]
    else:
        required += ["runtime/ReveriePlanner", "start.sh", "stop.sh"]
    if target.startswith("darwin"):
        required += ["start.command", "stop.command"]
    for name in required:
        if prefix + name not in entries:
            fail("missing required entry: " + name)

    manifest = entries[prefix + "BUILD-MANIFEST.txt"].decode("utf-8")
    header = {}
    hashes = {}
    for line in manifest.splitlines():
        if not line:
            continue
        digest_match = re.match(r"^([0-9a-f]{64})  (.+)$", line)
        if digest_match:
            hashes[digest_match.group(2)] = digest_match.group(1)
        elif ": " in line:
            key, value = line.split(": ", 1)
            header[key] = value
    if header.get("version") != version or header.get("target") != target:
        fail("BUILD-MANIFEST version/target does not match the archive")
    if header.get("files") != str(len(hashes)):
        fail("BUILD-MANIFEST file count does not match its entries")
    for name, digest in hashes.items():
        if prefix + name not in entries:
            fail("BUILD-MANIFEST lists a missing file: " + name)
        actual = hashlib.sha256(entries[prefix + name]).hexdigest()
        if actual != digest:
            fail("checksum mismatch for " + name)

    with open(args.archive, "rb") as stream:
        archive_digest = hashlib.sha256(stream.read()).hexdigest()
    print(json.dumps({
        "status": "ok",
        "archive": str(args.archive),
        "target": target,
        "version": version,
        "sourceCommit": header.get("source-commit", ""),
        "entries": len(names),
        "payloadFiles": len(hashes),
        "sha256": archive_digest,
        "sizeBytes": args.archive.stat().st_size,
    }, indent=2))


if __name__ == "__main__":
    main()
