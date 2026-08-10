.PHONY: build test fmt vet all release release-snapshot

all: fmt vet test build

build:
	go build -ldflags="-s -w" -o bin/agent-stripe ./cmd/agent-stripe

test:
	go test ./...

# -count=1 disables the test cache. Without it a repeat run prints "(cached)"
# for every package and executes nothing, which is indistinguishable from a
# real pass. The env guard matters for the same reason: with no key every
# integration test calls t.Skip and the package still prints "ok", so a
# credential-less run looks exactly like a green one.
integration:
	@if [ -z "$$STRIPE_TEST_KEY" ]; then \
		echo "STRIPE_TEST_KEY is not set."; \
		echo "Every integration test would skip and still report ok — refusing to run a no-op."; \
		echo "  STRIPE_TEST_KEY=sk_test_... STRIPE_TEST_CONNECTED_ACCOUNT=acct_... make integration"; \
		exit 1; \
	fi
	@if [ -z "$$STRIPE_TEST_CONNECTED_ACCOUNT" ]; then \
		echo "warning: STRIPE_TEST_CONNECTED_ACCOUNT unset — the Connect tests will skip."; \
		echo "         Prefer an account with requirements past due; the happy path exercises fewer branches."; \
	fi
	go test -tags=integration -count=1 ./...

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
