# Every target here is also what CI runs. If you change a command, change the
# workflow in the same commit — AGENTS.md names these as the verification set.
GO      ?= go
BIN     ?= bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/jacorbello/substrate/internal/version.Version=$(VERSION)

.PHONY: all build test lint vet fmt-check vuln smoke clean check

all: check

## check: everything CI's `go` job runs. Run this before opening a PR.
check: fmt-check vet lint test vuln

build:
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN)/ ./cmd/...

test:
	$(GO) test -race -covermode=atomic -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

vet:
	$(GO) vet ./...

lint:
	golangci-lint run

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

## smoke: build the real server, boot it against empty state, hit it.
smoke: build
	./scripts/smoke.sh

clean:
	rm -rf $(BIN) coverage.out
