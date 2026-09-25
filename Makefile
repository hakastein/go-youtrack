.PHONY: go openapi generate ytapi

go:
	test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	go test -race ./...

openapi:
	scripts/openapi.sh

generate:
	go generate ./...

ytapi:
	scripts/ytapi.sh
