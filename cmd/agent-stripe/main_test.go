package main

import "testing"

// TestRegistryWiring is a smoke-level regression guard: every command we ship
// in v1 must be present in the registry with a non-empty Usage string. The
// help_e2e_test runs each command's `help` token through the built binary;
// this test guards the wiring without paying the build cost.
func TestRegistryWiring(t *testing.T) {
	reg := buildRegistry()
	want := []string{
		"account", "balance", "charge", "checkout-session", "customer",
		"dispute", "event", "invoice", "invoice-item", "payment-intent",
		"payment-method", "payout", "price", "product", "refund", "resource",
		"setup-intent", "subscription", "subscription-item",
		"subscription-schedule", "transfer", "webhook-endpoint",
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
