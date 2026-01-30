#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

usage() {
  cat <<'EOF'
Usage: ./publish_version.sh

Publishes a `latest_version.json` file to GitHub Pages via a branch (default:
`gh-pages`). The file content is:

  {"version": "X.Y.Z"}

The version is read from the latest semver tag merged into origin/main (vX.Y.Z).
EOF
}

branch="gh-pages"

if [[ $# -gt 0 ]]; then
  case "${1:-}" in
    -h|--help) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
fi

if ! command -v git >/dev/null 2>&1; then
  echo "error: git is required" >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "error: python3 is required" >&2
  exit 1
fi

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "error: not in a git repository" >&2
  exit 1
fi
cd "$repo_root"

if ! git remote get-url origin >/dev/null 2>&1; then
  echo "error: remote \"origin\" is not configured" >&2
  exit 1
fi

git fetch origin main --tags

base_ref="origin/main"
if ! git rev-parse -q --verify "$base_ref" >/dev/null; then
  echo "error: missing ${base_ref} (fetch failed?)" >&2
  exit 1
fi

latest_tag="$(
  python3 - <<'PY'
import re
import subprocess
import sys

tags = subprocess.check_output(
  ["git", "tag", "-l", "v[0-9]*.[0-9]*.[0-9]*", "--merged", "origin/main"],
  text=True,
).splitlines()

best = None
best_key = None
pat = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")
for t in tags:
  m = pat.match(t.strip())
  if not m:
    continue
  key = tuple(int(x) for x in m.groups())
  if best is None or key > best_key:
    best = t.strip()
    best_key = key

if best is None:
  print("error: no vX.Y.Z tags merged into origin/main", file=sys.stderr)
  raise SystemExit(1)

print(best)
PY
)"

if ! git ls-remote --exit-code --tags origin "refs/tags/${latest_tag}" >/dev/null 2>&1; then
  echo "error: tag ${latest_tag} is not pushed to origin" >&2
  exit 1
fi

version="${latest_tag#v}"

tmpdir="$(mktemp -d)"
cleanup() {
  git worktree remove --force "$tmpdir" >/dev/null 2>&1 || true
  rm -rf "$tmpdir" >/dev/null 2>&1 || true
}
trap cleanup EXIT

json_path="latest_version.json"

if git ls-remote --exit-code --heads origin "$branch" >/dev/null 2>&1; then
  git fetch origin "$branch"
  git worktree add -f -B "$branch" "$tmpdir" "origin/$branch"
else
  git worktree add --detach "$tmpdir" HEAD
  git -C "$tmpdir" checkout --orphan "$branch"
  find "$tmpdir" -mindepth 1 -maxdepth 1 ! -name .git -exec rm -rf {} +
fi

printf '{"version": "%s"}\n' "$version" >"$tmpdir/$json_path"
: >"$tmpdir/.nojekyll"

git -C "$tmpdir" add "$json_path" .nojekyll

if git -C "$tmpdir" diff --cached --quiet; then
  echo "done: ${json_path} already up to date (version: ${version})"
  exit 0
fi

git -C "$tmpdir" commit -m "publish latest version ${latest_tag}" >/dev/null
git -C "$tmpdir" push -u origin "$branch" >/dev/null

echo "done: published ${json_path} (${latest_tag}) to origin/${branch}"
