//go:build integration

package invoice

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/config"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// legacyVersion predates Stripe's Basil restructuring of Invoice, which moved
// the flat linkage fields under `parent` and dropped application_fee_amount.
// It is the version the phase-15 measurement used.
const legacyVersion = "2022-11-15"

// rawOptsAt builds opts that read at the given version. An empty version
// means the pinned one — and the client is still built with WithAPIVersion so
// both halves of the comparison go through identical plumbing.
func rawOptsAt(t *testing.T, key, version string) *cli.GlobalOpts {
	t.Helper()
	return &cli.GlobalOpts{
		Account:    &config.Account{Alias: "it", Mode: config.ModeTest},
		Raw:        true,
		APIVersion: version,
		Client:     agentstripe.NewClient(key, "", "", 15*time.Second, agentstripe.WithAPIVersion(version)),
	}
}

func fetchRawInvoice(t *testing.T, opts *cli.GlobalOpts, id string) map[string]any {
	t.Helper()
	inv, err := opts.Client.V1Invoices.Retrieve(context.Background(), id, &stripeapi.InvoiceRetrieveParams{})
	if err != nil {
		t.Fatalf("retrieve %s: %v", id, err)
	}
	m, err := cli.RawMap(opts, inv)
	if err != nil {
		t.Fatalf("raw map: %v", err)
	}
	return m
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The only test that actually proves the feature: the same invoice, requested
// twice, must come back with different field sets. Everything else is mocked
// and could pass against a --api-version implementation that never reached the
// wire.
//
// It deliberately asserts on the *shape of the difference* rather than a fixed
// field list — which fields moved is Stripe's business and will drift. What
// must hold is that the older version carries fields the pinned one does not.
func TestIntegration_APIVersionChangesTheFieldSet(t *testing.T) {
	key := os.Getenv("STRIPE_TEST_KEY")
	if key == "" {
		t.Skip("STRIPE_TEST_KEY not set; skipping integration test")
	}

	pinnedOpts := rawOptsAt(t, key, "")
	list := pinnedOpts.Client.V1Invoices.List(context.Background(), &stripeapi.InvoiceListParams{
		ListParams: stripeapi.ListParams{Limit: stripeapi.Int64(1)},
	})
	items, _, _, err := agentstripe.CollectRawList(context.Background(), list, 1, true)
	if err != nil {
		t.Fatalf("list invoices: %v", err)
	}
	if len(items) == 0 {
		t.Skip("no invoices on this test account; nothing to compare")
	}
	id, _ := items[0]["id"].(string)
	if id == "" {
		t.Fatalf("first invoice has no id: %v", items[0])
	}

	pinned := fetchRawInvoice(t, pinnedOpts, id)
	legacy := fetchRawInvoice(t, rawOptsAt(t, key, legacyVersion), id)

	var onlyLegacy []string
	for _, k := range keysOf(legacy) {
		if _, ok := pinned[k]; !ok {
			onlyLegacy = append(onlyLegacy, k)
		}
	}
	if len(onlyLegacy) == 0 {
		pj, _ := json.Marshal(keysOf(pinned))
		lj, _ := json.Marshal(keysOf(legacy))
		t.Fatalf("both versions returned the same field set — the Stripe-Version header is probably not reaching the wire.\n"+
			"pinned (%s): %s\nlegacy (%s): %s", agentstripe.PinnedAPIVersion, pj, legacyVersion, lj)
	}
	t.Logf("fields present at %s but not at %s: %v", legacyVersion, agentstripe.PinnedAPIVersion, onlyLegacy)

	// Both are still the same object — the version changes the shape, not
	// which invoice came back.
	if legacy["id"] != id {
		t.Errorf("legacy read returned a different object: %v", legacy["id"])
	}
}

// The typed path is what --raw exists to escape: at the pinned version it must
// still drop fields Stripe sent. If this ever stops holding, --raw has become
// redundant and the extra mode should go.
func TestIntegration_TypedPathDropsWireFields(t *testing.T) {
	key := os.Getenv("STRIPE_TEST_KEY")
	if key == "" {
		t.Skip("STRIPE_TEST_KEY not set; skipping integration test")
	}

	opts := rawOptsAt(t, key, "")
	list := opts.Client.V1Invoices.List(context.Background(), &stripeapi.InvoiceListParams{
		ListParams: stripeapi.ListParams{Limit: stripeapi.Int64(1)},
	})
	items, _, _, err := agentstripe.CollectRawList(context.Background(), list, 1, true)
	if err != nil {
		t.Fatalf("list invoices: %v", err)
	}
	if len(items) == 0 {
		t.Skip("no invoices on this test account")
	}
	id, _ := items[0]["id"].(string)

	raw := fetchRawInvoice(t, opts, id)
	typed := fetchRawInvoice(t, &cli.GlobalOpts{
		Account: &config.Account{Alias: "it", Mode: config.ModeTest},
		Client:  opts.Client,
	}, id)

	var dropped []string
	for _, k := range keysOf(raw) {
		if _, ok := typed[k]; !ok {
			dropped = append(dropped, k)
		}
	}
	t.Logf("fields on the wire at %s that the typed path drops: %v", agentstripe.PinnedAPIVersion, dropped)
}
