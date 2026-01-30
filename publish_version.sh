#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

usage() {
  cat <<'EOF'
Usage: ./publish_version.sh

Publishes a `latest_version.json` file to GitHub Pages via a branch (default:
`gh-pages`). The file content is:

  {"version": "X.Y.Z"}

The version is read from internal/cli/cli.go (`var Version = "X.Y.Z"`).

Safety checks:
  - must be on main, clean, and up to date with origin/main
  - must have a local tag vX.Y.Z at HEAD, and that tag must be pushed
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

if ! git rev-parse --verify main >/dev/null 2>&1; then
  echo "error: branch \"main\" does not exist locally" >&2
  exit 1
fi

current_branch="$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
if [[ "$current_branch" != "main" ]]; then
  echo "error: must be on \"main\" (currently: ${current_branch:-detached})" >&2
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

version="$(
  python3 - "$version_file" <<'PY'
import pathlib
import re
import sys

path = pathlib.Path(sys.argv[1])
text = path.read_text(encoding="utf-8")
m = re.search(r'(?m)^var Version = "(\d+)\.(\d+)\.(\d+)"\s*$', text)
if not m:
  raise SystemExit(f'error: could not find `var Version = "X.Y.Z"` in {path}')
print(".".join(m.groups()))
PY
)"

if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: version must look like X.Y.Z (got: '$version')" >&2
  exit 2
fi

tag="v${version}"
if ! git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
  echo "error: missing local tag: ${tag}" >&2
  echo "run: git tag \"${tag}\"" >&2
  exit 1
fi

tag_commit="$(git rev-list -n 1 "${tag}")"
head_commit="$(git rev-parse HEAD)"
if [[ "$tag_commit" != "$head_commit" ]]; then
  echo "error: tag ${tag} is not at HEAD" >&2
  echo "tag:  $tag_commit" >&2
  echo "head: $head_commit" >&2
  exit 1
fi

if ! git ls-remote --exit-code --tags origin "refs/tags/${tag}" >/dev/null 2>&1; then
  echo "error: tag ${tag} is not pushed to origin" >&2
  echo "run: git push origin \"${tag}\"" >&2
  exit 1
fi

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

git -C "$tmpdir" commit -m "publish latest version v${version}" >/dev/null
git -C "$tmpdir" push -u origin "$branch" >/dev/null

echo "done: published ${json_path} (version: ${version}) to origin/${branch}"
