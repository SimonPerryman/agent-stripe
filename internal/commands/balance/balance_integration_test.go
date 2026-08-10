//go:build integration

package balance

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/config"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"
)

func TestIntegration_BalanceGet(t *testing.T) {
	key := os.Getenv("STRIPE_TEST_KEY")
	if key == "" {
		t.Skip("STRIPE_TEST_KEY not set; skipping integration test")
	}
	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "it", Mode: config.ModeTest},
		Client:  agentstripe.NewClient(key, "", "", 15*time.Second),
	}
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	go io.Copy(io.Discard, r)
	defer func() { w.Close(); os.Stdout = old }()
	if err := runGet(context.Background(), opts, nil); err != nil {
		t.Fatalf("runGet: %v", err)
	}
}

func TestIntegration_BalanceTransactionsList(t *testing.T) {
	key := os.Getenv("STRIPE_TEST_KEY")
	if key == "" {
		t.Skip("STRIPE_TEST_KEY not set; skipping integration test")
	}
	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "it", Mode: config.ModeTest},
		Client:  agentstripe.NewClient(key, "", "", 15*time.Second),
	}
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	go io.Copy(io.Discard, r)
	defer func() { w.Close(); os.Stdout = old }()
	if err := runTransactions(context.Background(), opts, []string{"--limit", "1"}); err != nil {
		t.Fatalf("runTransactions: %v", err)
	}
}

// TestIntegration_BalanceGet_ConnectedAccount is the canonical "why hasn't
// this merchant been paid out" entry point: the balance that matters lives on
// the connected account, not the platform.
func TestIntegration_BalanceGet_ConnectedAccount(t *testing.T) {
	key := os.Getenv("STRIPE_TEST_KEY")
	if key == "" {
		t.Skip("STRIPE_TEST_KEY not set; skipping integration test")
	}
	acct := os.Getenv("STRIPE_TEST_CONNECTED_ACCOUNT")
	if acct == "" {
		t.Skip("STRIPE_TEST_CONNECTED_ACCOUNT not set; skipping Connect integration test")
	}
	opts := &cli.GlobalOpts{
		Account:       &config.Account{Alias: "it", Mode: config.ModeTest},
		StripeAccount: acct,
		Client:        agentstripe.NewClient(key, "", acct, 15*time.Second),
	}
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	go io.Copy(io.Discard, r)
	defer func() { w.Close(); os.Stdout = old }()
	if err := runGet(context.Background(), opts, nil); err != nil {
		t.Fatalf("runGet: %v", err)
	}
}
