#!/usr/bin/env bash
set -euo pipefail

# Release validation: run goreleaser check and a snapshot build, validate every
# archive and its contents, then smoke the Linux amd64 artifact.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

if ! command -v goreleaser >/dev/null 2>&1; then
  echo "GoReleaser is not installed." >&2
  exit 1
fi

cleanup() {
  rm -rf "$REPO_ROOT/dist"
}
trap cleanup EXIT

cleanup

echo "Running GoReleaser config check..."
goreleaser check

echo "Running GoReleaser snapshot build..."
goreleaser release --snapshot --clean

echo "Validating release artifacts..."
python3 - "$REPO_ROOT" <<'PY'
import hashlib
import json
import os
import re
import stat
import struct
import subprocess
import sys
import tarfile
import zipfile

repo_root = sys.argv[1]
dist_dir = os.path.join(repo_root, 'dist')
artifacts_path = os.path.join(dist_dir, 'artifacts.json')
metadata_path = os.path.join(dist_dir, 'metadata.json')

with open(artifacts_path) as f:
    artifacts = json.load(f)
with open(metadata_path) as f:
    metadata = json.load(f)

version = metadata['version']

expected_targets = [
    {'goos': 'darwin', 'goarch': 'amd64', 'goarm': None, 'ext': 'tar.gz', 'binary': 'pbvex'},
    {'goos': 'darwin', 'goarch': 'arm64', 'goarm': None, 'ext': 'tar.gz', 'binary': 'pbvex'},
    {'goos': 'linux', 'goarch': 'amd64', 'goarm': None, 'ext': 'tar.gz', 'binary': 'pbvex'},
    {'goos': 'linux', 'goarch': 'arm64', 'goarm': None, 'ext': 'tar.gz', 'binary': 'pbvex'},
    {'goos': 'linux', 'goarch': 'arm', 'goarm': '7', 'ext': 'tar.gz', 'binary': 'pbvex'},
    {'goos': 'linux', 'goarch': 'ppc64le', 'goarm': None, 'ext': 'tar.gz', 'binary': 'pbvex'},
    {'goos': 'linux', 'goarch': 's390x', 'goarm': None, 'ext': 'tar.gz', 'binary': 'pbvex'},
    {'goos': 'windows', 'goarch': 'amd64', 'goarm': None, 'ext': 'zip', 'binary': 'pbvex.exe'},
    {'goos': 'windows', 'goarch': 'arm64', 'goarm': None, 'ext': 'zip', 'binary': 'pbvex.exe'},
]


def expected_name(target):
    arch = target['goarch']
    if target['goarm']:
        arch = f"{arch}v{target['goarm']}"
    return f"pbvex_{version}_{target['goos']}_{arch}.{target['ext']}"


expected_names = {expected_name(t): t for t in expected_targets}
required_files = {
    'LICENSE.md': os.path.join(repo_root, 'LICENSE.md'),
    'README.md': os.path.join(repo_root, 'README.md'),
    'CHANGELOG.md': os.path.join(repo_root, 'CHANGELOG.md'),
}

docs_root = os.path.join(repo_root, 'docs')
for root, dirnames, filenames in os.walk(docs_root):
    dirnames.sort()
    for filename in sorted(filenames):
        if not filename.endswith('.md'):
            continue
        path = os.path.join(root, filename)
        archive_name = os.path.relpath(path, repo_root).replace(os.sep, '/')
        required_files[archive_name] = path

required_sources = {name: open(path, 'rb').read() for name, path in required_files.items()}
windows_reserved = re.compile(r'^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9]|CLOCK\$)(\..*)?$', re.IGNORECASE)

MAX_MEMBER_SIZE = 100 * 1024 * 1024
MAX_TOTAL_UNCOMPRESSED = 500 * 1024 * 1024


def safe_member_name(name):
    if '\x00' in name:
        return False, 'NUL byte'
    if '\\' in name:
        return False, 'backslash'
    name = name.rstrip('/')
    if not name:
        return False, 'empty'
    if name.startswith('/'):
        return False, 'absolute'
    if re.match(r'^[A-Za-z]:', name):
        return False, 'Windows drive'
    if name.startswith('//'):
        return False, 'UNC/absolute'
    parts = name.split('/')
    if '..' in parts:
        return False, 'traversal'
    if not all(parts):
        return False, 'empty component'
    base = parts[-1]
    if windows_reserved.match(base):
        return False, f'Windows reserved name {base}'
    return True, ''


def validate_binary_magic(data, goos):
    if len(data) < 4:
        return False
    if goos == 'linux':
        return data[:4] == b'\x7fELF'
    if goos == 'darwin':
        return data[:4] in (
            b'\xcf\xfa\xed\xfe',
            b'\xce\xfa\xed\xfe',
            b'\xca\xfe\xba\xbe',
            b'\xbe\xba\xfe\xca',
            b'\xfe\xed\xfa\xcf',
            b'\xfe\xed\xfa\xce',
        )
    if goos == 'windows':
        if data[:2] != b'MZ':
            return False
        if len(data) < 64:
            return False
        e_lfanew = struct.unpack('<I', data[0x3c:0x40])[0]
        if e_lfanew + 4 > len(data):
            return False
        return data[e_lfanew:e_lfanew+4] == b'PE\x00\x00'
    return False


def check_size(total_ref, archive_name, size):
    if size > MAX_MEMBER_SIZE:
        print(f'Member in {archive_name} exceeds size limit: {size}', file=sys.stderr)
        sys.exit(1)
    total_ref[0] += size
    if total_ref[0] > MAX_TOTAL_UNCOMPRESSED:
        print(f'Total uncompressed size exceeds limit: {total_ref[0]}', file=sys.stderr)
        sys.exit(1)


# Index GoReleaser Binary artifacts by build target.
binary_artifacts = [a for a in artifacts if a.get('type') == 'Binary']
binary_by_target = {}
for ba in binary_artifacts:
    target = ba.get('target')
    if not target:
        continue
    if target in binary_by_target:
        print(f"Duplicate Binary artifact target {target}", file=sys.stderr)
        sys.exit(1)
    binary_by_target[target] = ba

archives = [a for a in artifacts if a.get('type') == 'Archive']
if len(archives) != len(expected_targets):
    print(f'Expected {len(expected_targets)} archives, got {len(archives)}', file=sys.stderr)
    sys.exit(1)

seen_targets = set()
total_uncompressed = [0]

for archive in archives:
    name = archive['name']
    if name not in expected_names:
        print(f'Unexpected archive name {name}', file=sys.stderr)
        sys.exit(1)
    target = expected_names[name]
    key = (target['goos'], target['goarch'], target['goarm'])
    if key in seen_targets:
        print(f'Duplicate target {key}', file=sys.stderr)
        sys.exit(1)
    seen_targets.add(key)

    if archive.get('goos') != target['goos'] or archive.get('goarch') != target['goarch']:
        print(f'Archive {name} OS/arch mismatch', file=sys.stderr)
        sys.exit(1)
    goarm = archive.get('goarm')
    if target['goarm'] is not None and str(goarm) != str(target['goarm']):
        print(f'Archive {name} arm version mismatch: {goarm} != {target["goarm"]}', file=sys.stderr)
        sys.exit(1)
    if archive.get('extra', {}).get('Binaries', [None])[0] != target['binary']:
        print(f'Archive {name} binary name mismatch', file=sys.stderr)
        sys.exit(1)
    fmt = archive.get('extra', {}).get('Format')
    if fmt != target['ext']:
        print(f'Archive {name} format mismatch: {fmt} != {target["ext"]}', file=sys.stderr)
        sys.exit(1)

    archive_target = archive.get('target')
    if not archive_target or archive_target not in binary_by_target:
        print(f'Archive {name} has no matching GoReleaser Binary artifact', file=sys.stderr)
        sys.exit(1)
    binary_artifact = binary_by_target[archive_target]
    binary_path = binary_artifact['path']
    if not os.path.isfile(binary_path):
        print(f'Binary artifact missing: {binary_path}', file=sys.stderr)
        sys.exit(1)
    binary_expected_size = os.path.getsize(binary_path)
    binary_expected_hash = hashlib.sha256(open(binary_path, 'rb').read()).hexdigest()

    archive_path = os.path.join(dist_dir, name)
    if not os.path.isfile(archive_path):
        print(f'Archive file missing: {archive_path}', file=sys.stderr)
        sys.exit(1)

    binary_member_name = target['binary']
    found_binary = False
    found_files = set()
    seen_members = set()

    if fmt == 'tar.gz':
        with tarfile.open(archive_path, 'r:gz') as tf:
            for member in tf.getmembers():
                if member.name in seen_members:
                    print(f'Archive {name} duplicate member {member.name}', file=sys.stderr)
                    sys.exit(1)
                seen_members.add(member.name)
                if member.issym() or member.islnk() or member.isdev():
                    print(f'Archive {name} contains symlink/hardlink/device: {member.name}', file=sys.stderr)
                    sys.exit(1)
                if not member.isfile() and not member.isdir():
                    print(f'Archive {name} contains unexpected member type: {member.name}', file=sys.stderr)
                    sys.exit(1)
                ok, reason = safe_member_name(member.name)
                if not ok:
                    print(f'Archive {name} unsafe member {member.name}: {reason}', file=sys.stderr)
                    sys.exit(1)
                if member.isdir():
                    continue
                check_size(total_uncompressed, name, member.size)
                data = tf.extractfile(member).read()
                if len(data) != member.size:
                    print(f'Archive {name} member {member.name} truncated', file=sys.stderr)
                    sys.exit(1)
                if member.name == binary_member_name:
                    if found_binary:
                        print(f'Archive {name} duplicate binary member', file=sys.stderr)
                        sys.exit(1)
                    if (member.mode & 0o111) == 0:
                        print(f'Archive {name} binary is not executable', file=sys.stderr)
                        sys.exit(1)
                    if not validate_binary_magic(data, target['goos']):
                        print(f'Archive {name} binary has invalid platform magic', file=sys.stderr)
                        sys.exit(1)
                    if len(data) != binary_expected_size:
                        print(f'Archive {name} binary size mismatch: {len(data)} != {binary_expected_size}', file=sys.stderr)
                        sys.exit(1)
                    actual_hash = hashlib.sha256(data).hexdigest()
                    if actual_hash != binary_expected_hash:
                        print(f'Archive {name} binary hash mismatch', file=sys.stderr)
                        sys.exit(1)
                    found_binary = True
                elif member.name in required_files:
                    if data != required_sources[member.name]:
                        print(f'Archive {name} member {member.name} does not match repository source', file=sys.stderr)
                        sys.exit(1)
                    found_files.add(member.name)
                else:
                    print(f'Archive {name} contains unexpected file {member.name}', file=sys.stderr)
                    sys.exit(1)
    elif fmt == 'zip':
        with zipfile.ZipFile(archive_path, 'r') as zf:
            for info in zf.infolist():
                fname = info.filename
                if fname in seen_members:
                    print(f'Archive {name} duplicate member {fname}', file=sys.stderr)
                    sys.exit(1)
                seen_members.add(fname)
                ok, reason = safe_member_name(fname)
                if not ok:
                    print(f'Archive {name} unsafe member {fname}: {reason}', file=sys.stderr)
                    sys.exit(1)
                if info.is_dir():
                    continue
                check_size(total_uncompressed, name, info.file_size)
                data = zf.read(fname)
                if len(data) != info.file_size:
                    print(f'Archive {name} member {fname} truncated', file=sys.stderr)
                    sys.exit(1)
                unix_mode = info.external_attr >> 16
                if unix_mode and not stat.S_ISREG(unix_mode):
                    print(f'Archive {name} member {fname} is not a regular file', file=sys.stderr)
                    sys.exit(1)
                if stat.S_ISLNK(unix_mode) or stat.S_ISBLK(unix_mode) or stat.S_ISCHR(unix_mode) or stat.S_ISFIFO(unix_mode) or stat.S_ISSOCK(unix_mode):
                    print(f'Archive {name} contains symlink/device: {fname}', file=sys.stderr)
                    sys.exit(1)
                if fname == binary_member_name:
                    if found_binary:
                        print(f'Archive {name} duplicate binary member', file=sys.stderr)
                        sys.exit(1)
                    if unix_mode and (unix_mode & 0o111) == 0:
                        print(f'Archive {name} binary is not executable', file=sys.stderr)
                        sys.exit(1)
                    if not validate_binary_magic(data, target['goos']):
                        print(f'Archive {name} binary has invalid platform magic', file=sys.stderr)
                        sys.exit(1)
                    if len(data) != binary_expected_size:
                        print(f'Archive {name} binary size mismatch: {len(data)} != {binary_expected_size}', file=sys.stderr)
                        sys.exit(1)
                    actual_hash = hashlib.sha256(data).hexdigest()
                    if actual_hash != binary_expected_hash:
                        print(f'Archive {name} binary hash mismatch', file=sys.stderr)
                        sys.exit(1)
                    found_binary = True
                elif fname in required_files:
                    if data != required_sources[fname]:
                        print(f'Archive {name} member {fname} does not match repository source', file=sys.stderr)
                        sys.exit(1)
                    found_files.add(fname)
                else:
                    print(f'Archive {name} contains unexpected file {fname}', file=sys.stderr)
                    sys.exit(1)
    else:
        print(f'Unknown archive format {fmt} for {name}', file=sys.stderr)
        sys.exit(1)

    if not found_binary:
        print(f'Archive {name} missing binary {binary_member_name}', file=sys.stderr)
        sys.exit(1)
    missing = set(required_files.keys()) - found_files
    if missing:
        print(f'Archive {name} missing required files: {missing}', file=sys.stderr)
        sys.exit(1)

# Verify checksums.txt and SHA-256 of every archive.
checksums_path = os.path.join(dist_dir, 'checksums.txt')
with open(checksums_path) as f:
    raw_checksums = f.read()

sha256_pattern = re.compile(r'^[0-9a-f]{64}$')
entries = {}
for line in raw_checksums.splitlines():
    line = line.rstrip()
    if not line:
        continue
    if '  ' not in line:
        print(f'Invalid checksum line: {line}', file=sys.stderr)
        sys.exit(1)
    sha, fname = line.split('  ', 1)
    if not sha256_pattern.match(sha):
        print(f'Checksum is not 64 lowercase hex chars for {fname}: {sha}', file=sys.stderr)
        sys.exit(1)
    if fname in entries:
        print(f'Duplicate checksum entry for {fname}', file=sys.stderr)
        sys.exit(1)
    entries[fname] = sha

expected_names_set = set(expected_names.keys())
if set(entries.keys()) != expected_names_set:
    print(f'Checksum entries mismatch: {set(entries.keys())} != {expected_names_set}', file=sys.stderr)
    sys.exit(1)

for archive in archives:
    path = os.path.join(dist_dir, archive['name'])
    h = hashlib.sha256()
    with open(path, 'rb') as f:
        for chunk in iter(lambda: f.read(8192), b''):
            h.update(chunk)
    actual = h.hexdigest()
    expected = entries[archive['name']]
    if actual != expected:
        print(f'Checksum mismatch for {archive["name"]}: {actual} != {expected}', file=sys.stderr)
        sys.exit(1)

# Safely extract the Linux amd64 binary.
linux_amd64_target = next(t for t in expected_targets if t['goos'] == 'linux' and t['goarch'] == 'amd64')
linux_amd64_name = expected_name(linux_amd64_target)
linux_amd64 = next(a for a in archives if a['name'] == linux_amd64_name)
extract_dir = os.path.join(dist_dir, 'extract_linux_amd64')
os.makedirs(extract_dir, exist_ok=True)
archive_path = os.path.join(dist_dir, linux_amd64_name)
fmt = linux_amd64['extra']['Format']
expected_binary = linux_amd64_target['binary']

if fmt == 'tar.gz':
    with tarfile.open(archive_path, 'r:gz') as tf:
        member = tf.getmember(expected_binary)
        if not member.isfile():
            print('Linux amd64 binary is not a regular file', file=sys.stderr)
            sys.exit(1)
        with open(os.path.join(extract_dir, expected_binary), 'wb') as out:
            out.write(tf.extractfile(expected_binary).read())
elif fmt == 'zip':
    with zipfile.ZipFile(archive_path, 'r') as zf:
        if expected_binary not in zf.namelist():
            print('Linux amd64 binary missing in zip', file=sys.stderr)
            sys.exit(1)
        with open(os.path.join(extract_dir, expected_binary), 'wb') as out:
            out.write(zf.read(expected_binary))
else:
    print(f'Unknown format {fmt}', file=sys.stderr)
    sys.exit(1)

binary = os.path.join(extract_dir, expected_binary)
if not os.path.isfile(binary):
    print('Extracted Linux amd64 binary missing', file=sys.stderr)
    sys.exit(1)
os.chmod(binary, 0o755)

print(f"Running smoke test with GoReleaser artifact version {version}...")
subprocess.run([
    os.path.join(repo_root, 'scripts', 'smoke.sh'),
    '--binary', binary,
    '--version', version,
], check=True)

print('Release validation passed.')
PY
