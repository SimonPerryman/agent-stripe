.PHONY: build test fmt vet all

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
