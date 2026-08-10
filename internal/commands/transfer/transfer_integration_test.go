//go:build integration

package transfer

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

func TestIntegration_TransferList(t *testing.T) {
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

// TestIntegration_TransferReversals follows the dispute pattern: pull the
// first transfer, list its reversals, and skip the body if the test account
// has none. Avoids a hard-coded fixture id.
func TestIntegration_TransferReversals(t *testing.T) {
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
	if err := runList(context.Background(), opts, []string{"--limit", "1"}); err != nil {
		w.Close()
		os.Stdout = old
		<-done
		t.Fatalf("runList for transfer id discovery: %v", err)
	}
	w.Close()
	os.Stdout = old
	<-done

	var env struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal transfer list: %v (out=%q)", err, buf.String())
	}
	if len(env.Data) == 0 {
		t.Skip("no transfers on test account; skipping reversals integration")
	}

	old = os.Stdout
	r, w, _ = os.Pipe()
	os.Stdout = w
	go io.Copy(io.Discard, r)
	defer func() { w.Close(); os.Stdout = old }()
	if err := runReversals(context.Background(), opts, []string{env.Data[0].ID, "--limit", "1"}); err != nil {
		t.Fatalf("runReversals: %v", err)
	}
}
