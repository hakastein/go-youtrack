#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Сборка YouTrack, на которой измерены факты о сервере в docs/adr: дев-инстанс ytrack (dev/).
readonly pinned=2026.1.13757
url=${YOUTRACK_URL:-http://localhost:8091}

config=$(curl -fsS "$url/api/config?fields=version,build")
version=$(sed -n 's/.*"version":"\([^"]*\)".*/\1/p' <<<"$config")
build=$(sed -n 's/.*"build":"\([^"]*\)".*/\1/p' <<<"$config")
if [[ $version.$build != "$pinned" ]]; then
	echo "$url: YouTrack $version.$build, а спецификация снимается с $pinned" >&2
	exit 1
fi

part=api/openapi.json.part
trap 'rm -f "$part"' EXIT
curl -fsS --create-dirs -o "$part" "$url/api/openapi.json"
mv "$part" api/openapi.json
