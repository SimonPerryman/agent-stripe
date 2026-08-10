//go:build integration

package account

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

// TestIntegration_AccountTest_ConnectProbe exercises the first step of any
// Connect investigation: `--stripe-account acct_... account test` must report
// the *connected* account, not the platform one. Getting this wrong looks
// exactly like success, which is why it is worth a live assertion.
func TestIntegration_AccountTest_ConnectProbe(t *testing.T) {
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
		Timeout:       15 * time.Second,
	}

	var buf bytes.Buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&buf, r); close(done) }()
	err := runTest(context.Background(), opts, nil)
	w.Close()
	os.Stdout = old
	<-done
	if err != nil {
		t.Fatalf("runTest: %v (out=%q)", err, buf.String())
	}

	var env struct {
		StripeAccount string `json:"stripeAccount"`
		Data          struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if uErr := json.Unmarshal(buf.Bytes(), &env); uErr != nil {
		t.Fatalf("unmarshal: %v (out=%q)", uErr, buf.String())
	}
	if env.Data.ID != acct {
		t.Errorf("probe returned account %q, want the connected account %q", env.Data.ID, acct)
	}
	if env.StripeAccount != acct {
		t.Errorf("envelope stripeAccount = %q, want %q", env.StripeAccount, acct)
	}
}
