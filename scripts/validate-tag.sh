#!/usr/bin/env bash
set -euo pipefail

# Validate that a release tag is a strict SemVer 2.0.0 tag, matches the
# checked-out commit, and is the exact version GoReleaser will publish.
# Use in CI before goreleaser release, or pass a tag name as the first argument.

# Validate SemVer 2.0.0 grammar in Python because bash ERE cannot accurately
# distinguish numeric and alphanumeric prerelease identifiers.
python3 - "${1:-${GITHUB_REF_NAME:-${CI_TAG:-}}}" "${VALIDATE_TAG_TEST:-0}" <<'PY'
import re
import sys

tag = sys.argv[1]
self_test = sys.argv[2] == '1'


def validate_semver(t):
    if not t.startswith('v'):
        return False
    core = t[1:]
    if '+' in core:
        core, build = core.split('+', 1)
    else:
        build = None
    if '-' in core:
        core, pre = core.split('-', 1)
    else:
        pre = None

    parts = core.split('.')
    if len(parts) != 3:
        return False
    for p in parts:
        if not re.fullmatch(r'[0-9]+', p) or (len(p) > 1 and p.startswith('0')):
            return False

    def valid_identifiers(s, allow_leading_zero):
        if s == '':
            return False
        if s.startswith('.') or s.endswith('.') or '..' in s:
            return False
        for ident in s.split('.'):
            if not ident:
                return False
            if not re.fullmatch(r'[0-9A-Za-z-]+', ident):
                return False
            if not allow_leading_zero and re.fullmatch(r'[0-9]+', ident):
                if len(ident) > 1 and ident.startswith('0'):
                    return False
        return True

    if pre is not None and not valid_identifiers(pre, allow_leading_zero=False):
        return False
    if build is not None and not valid_identifiers(build, allow_leading_zero=True):
        return False

    return True


if self_test:
    valid = [
        'v0.0.0',
        'v1.2.3',
        'v0.10.0-rc.1',
        'v1.0.0-alpha-01',
        'v1.0.0-alpha.1',
        'v1.0.0-0',
        'v1.0.0-0.1.2',
        'v1.0.0-alpha.1+build.123',
        'v1.0.0+01',
        'v2.0.0+exp.sha.5114f85',
    ]
    invalid = [
        'vfoo',
        'v1.2',
        'v1.2.3.4',
        'v1.2.03',
        'v01.2.3',
        'v1.2.3-',
        'v1.2.3+',
        'v1.2.3-rc..1',
        'v1.2.3_rc',
        'v1.2.3-01',
        'v1.2.3-alpha.01',
        'refs/tags/v1.2.3',
        'v1.2.3-α',
        'v1.2.3-α.1',
        'v1.2.3+1.é',
        'v1.2.3-¹',
        'v1.2.3-中文',
    ]
    for t in valid:
        if not validate_semver(t):
            print(f'Self-test expected {t} to be valid', file=sys.stderr)
            sys.exit(1)
    for t in invalid:
        if validate_semver(t):
            print(f'Self-test expected {t} to be invalid', file=sys.stderr)
            sys.exit(1)
    print('SemVer self-test passed.')
    sys.exit(0)

if not tag:
    print('No tag provided. Usage: validate-tag.sh <tag> (or set GITHUB_REF_NAME/CI_TAG)', file=sys.stderr)
    sys.exit(1)

if not validate_semver(tag):
    print(f"Invalid release tag '{tag}': expected strict SemVer 2.0.0 like v1.2.3 or v1.2.3-rc.1", file=sys.stderr)
    sys.exit(1)

print(f"Tag '{tag}' matches SemVer 2.0.0 grammar.")
PY

# If this was a self-test run, Python has already produced the result.
if [[ "${VALIDATE_TAG_TEST:-}" == "1" ]]; then
    exit 0
fi

tag="${1:-${GITHUB_REF_NAME:-${CI_TAG:-}}}"

if [[ -z "$tag" ]]; then
    echo "No tag provided. Usage: validate-tag.sh <tag> (or set GITHUB_REF_NAME/CI_TAG)" >&2
    exit 1
fi

if ! command -v git >/dev/null 2>&1; then
    echo "git is not available; cannot verify exact tag" >&2
    exit 1
fi

if ! git tag --points-at HEAD 2>/dev/null | grep -qxF "$tag"; then
    echo "Tag '$tag' does not point at the current HEAD" >&2
    echo "Tags at HEAD: $(git tag --points-at HEAD 2>/dev/null | tr '\n' ' ')" >&2
    exit 1
fi

echo "Tag '$tag' matches the checked-out commit."
