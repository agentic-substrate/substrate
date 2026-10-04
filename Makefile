.PHONY: build test check run dev docs

build:
	npm run build:web
	go build -trimpath -o bin/substrate ./cmd/substrate

test: build
	go test -race -shuffle=on -cover ./...
	npm run test:checks
	npm run test:smoke
	npm run test:browser

check: test
	go vet ./...
	test -z "$$(gofmt -l cmd internal)"
	npm run check:web
	npm run docs:build
	npm run check:docs

run: build
	./bin/substrate serve

dev:
	npm run dev

docs:
	npm run docs:dev
