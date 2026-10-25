.PHONY: all generate update-patches check-upstream sync-upstream test

all: generate test

generate:
	go generate ./pkg/analyzer/...

update-patches:
	go run ./cmd/upstreamchecker -config upstream.json -update-patches

check-upstream:
	./scripts/check-upstream.sh -target local

sync-upstream:
	./scripts/check-upstream.sh -update

test:
	go test -v ./...
