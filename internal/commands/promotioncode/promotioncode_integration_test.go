//go:build integration

package promotioncode

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

func TestIntegration_PromotionCodeList(t *testing.T) {
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

// The tri-state active filter is the one bit of local logic here, and only a
// real request proves both values are accepted rather than 400'd.
func TestIntegration_PromotionCodeList_ActiveFilters(t *testing.T) {
	key := os.Getenv("STRIPE_TEST_KEY")
	if key == "" {
		t.Skip("STRIPE_TEST_KEY not set; skipping integration test")
	}
	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "it", Mode: config.ModeTest},
		Client:  agentstripe.NewClient(key, "", "", 15*time.Second),
	}
	for _, flag := range []string{"--active", "--inactive"} {
		t.Run(flag, func(t *testing.T) {
			old := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w
			go io.Copy(io.Discard, r)
			defer func() { w.Close(); os.Stdout = old }()
			if err := runList(context.Background(), opts, []string{flag, "--limit", "1"}); err != nil {
				t.Fatalf("runList %s: %v", flag, err)
			}
		})
	}
}
