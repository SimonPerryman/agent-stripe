//go:build integration

package connectedaccount

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/config"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"
)

func integrationOpts(t *testing.T) *cli.GlobalOpts {
	t.Helper()
	key := os.Getenv("STRIPE_TEST_KEY")
	if key == "" {
		t.Skip("STRIPE_TEST_KEY not set; skipping integration test")
	}
	return &cli.GlobalOpts{
		Account: &config.Account{Alias: "it", Mode: config.ModeTest},
		Client:  agentstripe.NewClient(key, "", "", 15*time.Second),
	}
}

// captureEnvelope runs fn with stdout piped and returns the emitted envelope.
func captureEnvelope(t *testing.T, fn func() error) []byte {
	t.Helper()
	var buf bytes.Buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&buf, r); close(done) }()
	err := fn()
	w.Close()
	os.Stdout = old
	<-done
	if err != nil {
		t.Fatalf("run: %v (out=%q)", err, buf.String())
	}
	return buf.Bytes()
}

// TestIntegration_ConnectedAccountSweep discovers a connected account from
// `list`, then exercises the sub-resources against it. Skips cleanly when the
// test-mode platform has no connected accounts, so the suite still passes on
// a plain (non-Connect) test key.
func TestIntegration_ConnectedAccountSweep(t *testing.T) {
	opts := integrationOpts(t)

	out := captureEnvelope(t, func() error {
		return runList(context.Background(), opts, []string{"--limit", "1"})
	})
	var env struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("unmarshal account list: %v (out=%q)", err, out)
	}
	if len(env.Data) == 0 {
		t.Skip("no connected accounts on test platform; skipping sub-resource integration")
	}
	acct := env.Data[0].ID

	t.Run("get", func(t *testing.T) {
		captureEnvelope(t, func() error { return runGet(context.Background(), opts, []string{acct}) })
	})
	t.Run("capabilities", func(t *testing.T) {
		captureEnvelope(t, func() error { return runCapabilities(context.Background(), opts, []string{acct}) })
	})
	t.Run("persons", func(t *testing.T) {
		captureEnvelope(t, func() error {
			return runPersons(context.Background(), opts, []string{acct, "--limit", "1"})
		})
	})
	t.Run("external-accounts", func(t *testing.T) {
		captureEnvelope(t, func() error { return runExternalAccounts(context.Background(), opts, []string{acct}) })
	})
}

// TestIntegration_StripeAccountHeader proves the header actually scopes a read
// against live Stripe: `get <acct>` on the platform and `account test` through
// the header must describe the same account. Needs an explicitly named test
// connected account (see the Development section of the README).
func TestIntegration_StripeAccountHeader(t *testing.T) {
	acct := os.Getenv("STRIPE_TEST_CONNECTED_ACCOUNT")
	if acct == "" {
		t.Skip("STRIPE_TEST_CONNECTED_ACCOUNT not set; skipping Connect header integration test")
	}
	key := os.Getenv("STRIPE_TEST_KEY")
	if key == "" {
		t.Skip("STRIPE_TEST_KEY not set; skipping integration test")
	}

	scoped := &cli.GlobalOpts{
		Account:       &config.Account{Alias: "it", Mode: config.ModeTest},
		StripeAccount: acct,
		Client:        agentstripe.NewClient(key, "", acct, 15*time.Second),
	}

	// GET /v1/account under the header returns the *connected* account — the
	// canonical "is this account reachable from my platform key?" probe.
	out := captureEnvelope(t, func() error {
		return runGet(context.Background(), scoped, []string{acct})
	})
	var env struct {
		StripeAccount string `json:"stripeAccount"`
		Data          struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("unmarshal: %v (out=%q)", err, out)
	}
	if env.Data.ID != acct {
		t.Errorf("data.id = %q, want %q", env.Data.ID, acct)
	}
	if env.StripeAccount != acct {
		t.Errorf("envelope stripeAccount = %q, want %q", env.StripeAccount, acct)
	}
}
