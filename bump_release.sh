#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

usage() {
  cat <<'EOF'
Usage: ./bump_release.sh [minor|fix]

Defaults to "minor".
EOF
}

bump_kind="${1:-minor}"
case "$bump_kind" in
  minor|fix) ;;
  -h|--help) usage; exit 0 ;;
  *) usage; exit 2 ;;
esac

if ! command -v git >/dev/null 2>&1; then
  echo "error: git is required" >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "error: python3 is required" >&2
  exit 1
fi
if ! command -v gofmt >/dev/null 2>&1; then
  echo "error: gofmt is required" >&2
  exit 1
fi

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "error: not in a git repository" >&2
  exit 1
fi
cd "$repo_root"

if ! git rev-parse --verify main >/dev/null 2>&1; then
  echo "error: branch \"main\" does not exist locally" >&2
  exit 1
fi

current_branch="$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
if [[ "$current_branch" != "main" ]]; then
  echo "error: must be on \"main\" (currently: ${current_branch:-detached})" >&2
  exit 1
fi

if ! git remote get-url origin >/dev/null 2>&1; then
  echo "error: remote \"origin\" is not configured" >&2
  exit 1
fi

if [[ -n "$(git status --porcelain=v1)" ]]; then
  echo "error: working tree is not clean" >&2
  git status --porcelain=v1 >&2
  exit 1
fi

git fetch origin main --tags

local_head="$(git rev-parse HEAD)"
remote_head="$(git rev-parse origin/main)"
if [[ "$local_head" != "$remote_head" ]]; then
  echo "error: local main is not up to date with origin/main" >&2
  echo "local:  $local_head" >&2
  echo "remote: $remote_head" >&2
  exit 1
fi

version_file="internal/cli/cli.go"
if [[ ! -f "$version_file" ]]; then
  echo "error: missing version file: $version_file" >&2
  exit 1
fi

versions_out="$(
  python3 - "$version_file" "$bump_kind" <<'PY'
import pathlib
import re
import sys

path = pathlib.Path(sys.argv[1])
bump = sys.argv[2]

text = path.read_text(encoding="utf-8")
m = re.search(r'(?m)^var Version = "(\d+)\.(\d+)\.(\d+)"\s*$', text)
if not m:
  raise SystemExit(f'error: could not find `var Version = "X.Y.Z"` in {path}')

major, minor, fix = map(int, m.groups())
current = f"{major}.{minor}.{fix}"

if bump == "minor":
  minor += 1
  fix = 0
elif bump == "fix":
  fix += 1
else:
  raise SystemExit(f"error: unknown bump kind: {bump}")

new = f"{major}.{minor}.{fix}"
print(f"{current} {new}")
PY
)"
IFS=' ' read -r current_version new_version <<<"$versions_out"
if [[ -z "${current_version}" || -z "${new_version}" || "${current_version}" == "${new_version}" ]]; then
  echo "error: failed to compute next version (got: '${versions_out}')" >&2
  exit 1
fi

tag="v${new_version}"
if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
  echo "error: tag already exists: ${tag}" >&2
  exit 1
fi

python3 - "$version_file" "$current_version" "$new_version" <<'PY'
import pathlib
import re
import sys

path = pathlib.Path(sys.argv[1])
current = sys.argv[2]
new = sys.argv[3]

text = path.read_text(encoding="utf-8")
pattern = rf'(?m)^var Version = "{re.escape(current)}"\s*$'
if not re.search(pattern, text):
  raise SystemExit(f'error: expected `var Version = "{current}"` in {path}')

updated = re.sub(
  pattern,
  f'var Version = "{new}"',
  text,
  count=1,
)
if updated == text:
  raise SystemExit(f"error: version update produced no change in {path}")

path.write_text(updated, encoding="utf-8")
PY

gofmt -w "$version_file"

if git diff --quiet -- "$version_file"; then
  echo "error: version bump produced no git diff in $version_file" >&2
  exit 1
fi

git add "$version_file"
git commit -m "bump version to ${new_version}"

git tag "${tag}"

git push origin main
git push origin "${tag}"

cat <<EOF
done: bumped to ${new_version} and pushed main + ${tag}

next:
  - verify locally (ex: run goagentbench on it; manually test it)
  - publish (update GitHub Pages latest_version.json):
      ./publish_version.sh
EOF
