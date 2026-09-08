# check/build/smoke/sqlc are also what CI runs. ko-build is operator-side
# (needs a local daemon and must not push). If you change a CI command, change
# the workflow in the same commit — AGENTS.md names these as the verification set.
GO      ?= go
BIN     ?= bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/agentic-substrate/substrate/internal/version.Version=$(VERSION)

.PHONY: all build test lint vet fmt-check vuln smoke clean check sqlc ko-build ko-push

all: check

## check: everything CI's `go` job runs. Run this before opening a PR.
check: fmt-check vet lint test vuln

build:
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN)/ ./cmd/...

test:
	$(GO) test -p 1 -race -covermode=atomic -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

vet:
	$(GO) vet ./...

lint:
	golangci-lint run

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

## sqlc: regenerate internal/store from migrations/ and queries.sql.
sqlc:
	sqlc generate

## ko-build: distroless substrate-server image into the local daemon. Does not push.
## Local inspection only: `ko.local/substrate-server:latest` is a mutable tag,
## which scripts/check-deploy-placeholders.sh --substituted rejects, and a
## containerd node cannot pull from your local Docker daemon. Use ko-push for
## anything that is going into deploy/server/deployment.yaml.
KO_DOCKER_REPO ?= ko.local
ko-build:
	@command -v ko >/dev/null || { echo "ko-build: ko not installed. Fix: go install github.com/ko-build/ko@latest"; exit 1; }
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko build --local ./cmd/substrate-server

## ko-push: build and PUSH substrate-server, printing repo/name@sha256:… on
## stdout. That digest is what goes in deployment.yaml. Requires an explicit
## KO_DOCKER_REPO; it refuses to run against the ko.local default so this
## target can never push somewhere by accident.
ko-push:
	@command -v ko >/dev/null || { echo "ko-push: ko not installed. Fix: go install github.com/ko-build/ko@latest"; exit 1; }
	@if [ "$(KO_DOCKER_REPO)" = "ko.local" ]; then \
		echo "ko-push: set KO_DOCKER_REPO to a real registry, e.g. make ko-push KO_DOCKER_REPO=ghcr.io/you"; exit 1; \
	fi
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko build --bare --tags= ./cmd/substrate-server

## smoke: boot the real server with no database; /healthz 200, /readyz 503.
smoke: build
	./scripts/smoke.sh

clean:
	rm -rf $(BIN) coverage.out
