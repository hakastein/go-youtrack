#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

readonly min_methods=335
readonly grep_no_match=1
readonly adapter=ytapi.go
readonly tree=$PWD

work=$(mktemp -d "${TMPDIR:-/tmp}/ytapi.XXXXXXXXXX")
trap 'rm -rf "$work"' EXIT
readonly copy=$work/copy

existing_only() {
	while IFS= read -r -d '' path; do
		if [[ -e $path || -L $path ]]; then
			printf '%s\0' "$path"
		fi
	done
}

mkdir "$copy"
git ls-files -z --cached --others --exclude-standard | existing_only |
	tar --null -cf - -T - | tar -xf - -C "$copy"
cd "$copy"

rm -rf ytapi catalogue.gen.go
if ! go generate ./...; then
	echo "FAIL  go generate"
	exit 1
fi

failed=0
verdict() {
	local name=$1 code=$2
	if ((code == 0)); then
		echo "ok    $name"
	else
		echo "FAIL  $name"
		failed=1
	fi
}

check() {
	local name=$1 code=0
	shift
	"$@" || code=$?
	verdict "$name" "$code"
}

stub_applied() {
	local code=0
	grep -rq ClientWithResponses ytapi || code=$?
	[[ $code -eq $grep_no_match ]]
}

interface_floor() {
	local methods
	methods=$(go doc ./ytapi ClientInterface | grep -cE '^[[:space:]]+[A-Z][A-Za-z0-9_]*\(ctx context\.Context') || true
	echo "ClientInterface: $methods"
	((methods >= min_methods))
}

outputs_match() {
	diff -rq "$tree/ytapi" "$copy/ytapi"
}

catalogue_matches() {
	diff -q "$tree/catalogue.gen.go" "$copy/catalogue.gen.go"
}

# Внутри модуля сгенерированный клиент виден только из адаптера: там все проверки отправки.
only_the_adapter_imports_ytapi() {
	local importers
	importers=$(grep -rl --include='*.go' '"github.com/hakastein/youtrack/ytapi"' . | grep -v '^./ytapi/' | sort) || true
	[[ $importers == "./$adapter" ]] || { echo "$importers"; return 1; }
}

# oapi-codegen выходит с 0 и на несобираемом пакете, и на пустом ClientInterface.
check "go build ./ytapi" go build ./ytapi
check "выхлоп в дереве совпадает с тем, что дают входы" outputs_match
check "каталог схем в дереве совпадает с тем, что даёт спецификация" catalogue_matches
check "ClientWithResponses в выхлопе нет" stub_applied
check "ClientInterface не меньше $min_methods методов" interface_floor
check "ytapi внутри модуля импортирует только $adapter" only_the_adapter_imports_ytapi
exit "$failed"
