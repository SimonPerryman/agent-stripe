//go:build integration

package coupon

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

func TestIntegration_CouponList(t *testing.T) {
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
	if err := runList(context.Background(), opts, []string{"--limit", "1"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
}

// TestIntegration_CouponGet discovers an id from the list rather than hard-coding
// a fixture — coupon ids are caller-chosen strings, so there is no id shape a
// test could assume.
func TestIntegration_CouponGet(t *testing.T) {
	key := os.Getenv("STRIPE_TEST_KEY")
	if key == "" {
		t.Skip("STRIPE_TEST_KEY not set; skipping integration test")
	}
	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "it", Mode: config.ModeTest},
		Client:  agentstripe.NewClient(key, "", "", 15*time.Second),
	}

	var buf bytes.Buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&buf, r); close(done) }()
	err := runList(context.Background(), opts, []string{"--limit", "1"})
	_ = w.Close()
	os.Stdout = old
	<-done
	if err != nil {
		t.Fatalf("runList for id discovery: %v", err)
	}

	var env struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal coupon list: %v (out=%q)", err, buf.String())
	}
	if len(env.Data) == 0 {
		t.Skip("no coupons on test account; skipping get integration")
	}

	old = os.Stdout
	r, w, _ = os.Pipe()
	os.Stdout = w
	go io.Copy(io.Discard, r)
	defer func() { w.Close(); os.Stdout = old }()
	if err := runGet(context.Background(), opts, []string{env.Data[0].ID}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
}

// Coupons do not inherit from the platform, so listing them under a connected
// account must succeed on its own — an empty list there is a real answer, not
// an error.
func TestIntegration_CouponList_ConnectedAccount(t *testing.T) {
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
	if err := runList(context.Background(), opts, []string{"--limit", "1"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
}
