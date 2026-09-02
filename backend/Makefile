.PHONY: build generate verify-generate test test-unit test-integration test-e2e

VERSION ?= $(shell ./scripts/resolve-version.sh)
LDFLAGS ?= -s -w -X main.Version=$(VERSION)

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -trimpath -o bin/server ./cmd/server

generate:
	go generate ./ent
	go generate ./cmd/server

# verify-generate fails when a committed ent artifact no longer matches
# ent/schema. Editing a schema field without regenerating shifts the positional
# descriptor lookups in ent/runtime/runtime.go, which only blows up when the
# process starts — see ent/runtime/bindings_test.go.
#
# Scoped to ./ent on purpose: `go generate ./cmd/server` runs the wire binary,
# whose own dependencies are not tracked in go.sum, so it cannot run from a
# clean checkout.
verify-generate:
	go generate ./ent
	@if [ -n "$$(git status --porcelain -- ent)" ]; then \
		echo "generated ent code is out of date; run 'make generate' and commit the result:"; \
		git status --porcelain -- ent; \
		exit 1; \
	fi

test:
	go test ./...
	golangci-lint run ./...

test-unit:
	go test -tags=unit ./...

test-integration:
	go test -tags=integration ./...

test-e2e:
	./scripts/e2e-test.sh

test-e2e-local:
	go test -tags=e2e -v -timeout=300s ./internal/integration/...
