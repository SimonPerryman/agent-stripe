package main

import (
	"strings"
	"testing"
)

// TestRegistryWiring is a smoke-level regression guard: every command we ship
// in v1 must be present in the registry with a non-empty Usage string. The
// help_e2e_test runs each command's `help` token through the built binary;
// this test guards the wiring without paying the build cost.
func TestRegistryWiring(t *testing.T) {
	reg := buildRegistry()
	want := []string{
		"account", "application-fee", "balance", "charge", "checkout-session",
		"connected-account", "coupon", "customer", "dispute", "event",
		"invoice", "invoice-item", "payment-intent", "payment-method",
		"payout", "price", "product", "promotion-code", "refund", "resource",
		"setup-intent", "subscription", "subscription-item",
		"subscription-schedule", "test-clock", "transfer", "webhook-endpoint",
	}
	if len(reg.Commands) != len(want) {
		t.Errorf("expected %d commands, got %d", len(want), len(reg.Commands))
	}
	for _, name := range want {
		spec, ok := reg.Commands[name]
		if !ok {
			t.Errorf("missing command %q", name)
			continue
		}
		if spec.Run == nil {
			t.Errorf("%q: Run is nil", name)
		}
		if spec.Usage == "" {
			t.Errorf("%q: Usage is empty", name)
		}
	}
	if !reg.Commands["resource"].NoAccount {
		t.Error("resource should be marked NoAccount=true")
	}
	if reg.Commands["charge"].NoAccount {
		t.Error("charge should require an account (NoAccount=false)")
	}
}

// TestSignalContextNoSignal verifies signalContext returns a context that
// becomes done after cancel() is called — without firing a real signal
// (which would invoke os.Exit and kill the test). Confirms the wiring of
// context.WithCancel under the signal handler.
func TestSignalContextNoSignal(t *testing.T) {
	ctx, cancel := signalContext()
	if ctx.Err() != nil {
		t.Fatalf("expected fresh context, got err=%v", ctx.Err())
	}
	cancel()
	if ctx.Err() == nil {
		t.Error("expected context to be cancelled after cancel()")
	}
}

// TestEveryNetworkCommandDocumentsConnect keeps Connect guidance from
// silently lagging behind the command surface.
//
// The --stripe-account flag is global and its effect is per-resource, so an
// agent reading one command's usage has no way to learn the flag applies
// there too. The first pass of Connect support documented six commands and
// missed fourteen — including `dispute`, where the platform-scoped lookup
// returns nothing for a disputed direct charge and reads as "no dispute".
func TestEveryNetworkCommandDocumentsConnect(t *testing.T) {
	// Commands that make no Stripe request, so account scope is meaningless.
	exempt := map[string]bool{"resource": true}

	for name, spec := range buildRegistry().Commands {
		if exempt[name] {
			continue
		}
		if !strings.Contains(spec.Usage, "--stripe-account") {
			t.Errorf("%s: usage never mentions --stripe-account; say how Connect affects this resource, or add it to the exempt list with a reason", name)
		}
	}
}
