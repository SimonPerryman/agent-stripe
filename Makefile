.PHONY: build test fmt vet all release release-snapshot

all: fmt vet test build

build:
	go build -ldflags="-s -w" -o bin/agent-stripe ./cmd/agent-stripe

test:
	go test ./...

integration:
	go test -tags=integration ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

# Cut a release. Requires a clean working tree, a pushed git tag (e.g. v0.1.0),
# and goreleaser on PATH. Publishes binaries to the GitHub release and a
# formula to simonperryman/homebrew-tap.
release:
	goreleaser release --clean

# Local dry-run: build all artifacts in dist/ without pushing anywhere.
release-snapshot:
	goreleaser release --clean --snapshot --skip=publish
