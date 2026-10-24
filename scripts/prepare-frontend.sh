#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mkdir -p "$repo_root/.local" "$repo_root/internal/web"
build_dir=$(mktemp -d "$repo_root/.local/frontend.XXXXXX")
trap 'rm -rf "$build_dir"' EXIT

curl --fail --location --silent --show-error "$1" -o "$build_dir/frontend.tar.gz"
mkdir "$build_dir/unpacked"
tar -xzf "$build_dir/frontend.tar.gz" -C "$build_dir/unpacked"
if [ ! -f "$build_dir/unpacked/dist/index.html" ]; then
    printf 'Frontend archive does not contain dist/index.html\n' >&2
    exit 1
fi

rm -rf "$repo_root/internal/web/dist"
mv "$build_dir/unpacked/dist" "$repo_root/internal/web/dist"
