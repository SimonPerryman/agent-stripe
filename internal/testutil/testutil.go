// Package testutil holds tiny test helpers shared across the resource command
// packages. They can't live in a _test.go helper because each test lives in a
// different package.
package testutil

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/config"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"
)

// CaptureStdout redirects os.Stdout to a discard pipe for the duration of the
// test. Restoration is registered via t.Cleanup so a panicking test still
// puts stdout back. Tests use it to keep `go test` output clean — they assert
// on responses through other channels (test servers).
func CaptureStdout(t *testing.T) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	t.Cleanup(func() {
		_ = w.Close()
		os.Stdout = old
		<-done
	})
}

// NewOpts builds a *cli.GlobalOpts targeting a test httptest server. The
// returned opts is populated with a deterministic test-mode account so list
// commands can fill the envelope's mode/account fields without crashing.
func NewOpts(baseURL string) *cli.GlobalOpts {
	return NewOptsForAccount(baseURL, "")
}

// NewOptsForAccount is NewOpts scoped to a connected account, so a command
// test can assert the Stripe-Account header reaches the wire. Threading the
// parameter through this one helper is what gives every command package
// Connect coverage without per-package changes.
func NewOptsForAccount(baseURL, stripeAccount string) *cli.GlobalOpts {
	return &cli.GlobalOpts{
		Account:       &config.Account{Alias: "test", Mode: config.ModeTest},
		StripeAccount: stripeAccount,
		Client:        agentstripe.NewClient("sk_test_fake", baseURL, stripeAccount, 5*time.Second),
	}
}
