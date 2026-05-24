//go:build integration

package charge

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

func TestIntegration_ChargeList(t *testing.T) {
	key := os.Getenv("STRIPE_TEST_KEY")
	if key == "" {
		t.Skip("STRIPE_TEST_KEY not set; skipping integration test")
	}
	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "it", Mode: config.ModeTest},
		Client:  agentstripe.NewClient(key, "", 15*time.Second),
	}
	restore := silenceStdout(t)
	defer restore()
	if err := runList(context.Background(), opts, []string{"--limit", "1"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
}

func silenceStdout(t *testing.T) func() {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	go io.Copy(io.Discard, r)
	return func() {
		w.Close()
		os.Stdout = old
	}
}
