//go:build integration

package applicationfee

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

// TestIntegration_ApplicationFeeSweep lists the platform's fees and, if any
// exist, follows the first one into get + refunds. Skips cleanly on a test
// key that has never taken a direct charge.
func TestIntegration_ApplicationFeeSweep(t *testing.T) {
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
	w.Close()
	os.Stdout = old
	<-done
	if err != nil {
		t.Fatalf("runList: %v (out=%q)", err, buf.String())
	}

	var env struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if uErr := json.Unmarshal(buf.Bytes(), &env); uErr != nil {
		t.Fatalf("unmarshal application fee list: %v (out=%q)", uErr, buf.String())
	}
	if len(env.Data) == 0 {
		t.Skip("no application fees on test account; skipping get/refunds integration")
	}
	feeID := env.Data[0].ID

	old = os.Stdout
	r, w, _ = os.Pipe()
	os.Stdout = w
	go io.Copy(io.Discard, r)
	defer func() { w.Close(); os.Stdout = old }()
	if err := runGet(context.Background(), opts, []string{feeID}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
	if err := runRefunds(context.Background(), opts, []string{feeID, "--limit", "1"}); err != nil {
		t.Fatalf("runRefunds: %v", err)
	}
}
