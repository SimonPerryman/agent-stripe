//go:build integration

package commands_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/commands/charge"
	"github.com/simonperryman/agent-stripe/internal/commands/customer"
	"github.com/simonperryman/agent-stripe/internal/commands/invoice"
	"github.com/simonperryman/agent-stripe/internal/commands/paymentintent"
	"github.com/simonperryman/agent-stripe/internal/commands/price"
	"github.com/simonperryman/agent-stripe/internal/commands/product"
	"github.com/simonperryman/agent-stripe/internal/commands/subscription"
	"github.com/simonperryman/agent-stripe/internal/config"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"
)

// TestIntegration_SearchSweep runs `<resource> search` against the live test-
// mode account for every searchable resource. Asserts request shape and
// envelope shape, not data — Stripe Search is eventually consistent (~1 min
// lag) so 0 results is acceptable.
func TestIntegration_SearchSweep(t *testing.T) {
	key := os.Getenv("STRIPE_TEST_KEY")
	if key == "" {
		t.Skip("STRIPE_TEST_KEY not set; skipping integration test")
	}

	type runFn func(context.Context, *cli.GlobalOpts, []string) error
	cases := []struct {
		name  string
		run   runFn
		query string
	}{
		{"charge", charge.Run, `status:"succeeded"`},
		{"customer", customer.Run, `email:"a@example.com"`},
		{"payment-intent", paymentintent.Run, `status:"succeeded"`},
		{"subscription", subscription.Run, `status:"active"`},
		{"invoice", invoice.Run, `status:"paid"`},
		{"product", product.Run, `active:"true"`},
		{"price", price.Run, `active:"true"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := &cli.GlobalOpts{
				Account: &config.Account{Alias: "it", Mode: config.ModeTest},
				Client:  agentstripe.NewClient(key, "", 15*time.Second),
			}
			out, err := captureRun(t, func() error {
				return tc.run(context.Background(), opts, []string{"search", "--query", tc.query, "--limit", "1"})
			})
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			// Envelope shape: top-level object with mode/account/api_version
			// and a data array. Stripe search returning 0 results is fine.
			env := map[string]any{}
			if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); jerr != nil {
				t.Fatalf("decode envelope: %v\nbody: %s", jerr, out)
			}
			if _, ok := env["data"]; !ok {
				t.Errorf("envelope missing data: %s", out)
			}
			if env["apiVersion"] == nil {
				t.Errorf("envelope missing apiVersion: %s", out)
			}
		})
	}
}

func captureRun(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		b, e := io.ReadAll(r)
		done <- result{b, e}
	}()
	runErr := fn()
	_ = w.Close()
	os.Stdout = old
	res := <-done
	if res.err != nil {
		t.Fatal(res.err)
	}
	return string(res.data), runErr
}
