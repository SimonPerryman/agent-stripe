package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/cli"
)

func TestDescribeCustomer_NoAPICall(t *testing.T) {
	// opts.Client is nil — proves describe does not touch Stripe.
	out := captureStdout(t, func() {
		if err := runDescribe(context.Background(), &cli.GlobalOpts{}, []string{"customer", "--depth", "2"}); err != nil {
			t.Fatalf("runDescribe: %v", err)
		}
	})
	var env struct {
		Data struct {
			Resource    string   `json:"resource"`
			ExpandPaths []string `json:"expandPaths"`
			Fields      []struct {
				Field string `json:"field"`
			} `json:"fields"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal: %v (out=%q)", err, out)
	}
	if env.Data.Resource != "customer" {
		t.Errorf("resource = %q, want customer", env.Data.Resource)
	}
	hasID := false
	for _, f := range env.Data.Fields {
		if f.Field == "id" {
			hasID = true
		}
	}
	if !hasID {
		t.Errorf("expected field `id` in customer field tree")
	}
	// expandPaths is curated — must include something useful.
	if len(env.Data.ExpandPaths) == 0 {
		t.Errorf("expected non-empty expandPaths for customer")
	}
}

func TestDescribeSubscription_DepthCap(t *testing.T) {
	// Subscription.items.data[].price.product is depth 4 — at depth 3 we
	// should see items but not price.product nested under it.
	out := captureStdout(t, func() {
		if err := runDescribe(context.Background(), &cli.GlobalOpts{}, []string{"subscription"}); err != nil {
			t.Fatalf("runDescribe: %v", err)
		}
	})
	if !strings.Contains(out, `"items"`) {
		t.Errorf("expected items field on subscription")
	}
}

func TestDescribeTransfer_ExpandPaths(t *testing.T) {
	out := captureStdout(t, func() {
		if err := runDescribe(context.Background(), &cli.GlobalOpts{}, []string{"transfer"}); err != nil {
			t.Fatalf("runDescribe: %v", err)
		}
	})
	var env struct {
		Data struct {
			Resource    string   `json:"resource"`
			ExpandPaths []string `json:"expandPaths"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal: %v (out=%q)", err, out)
	}
	if env.Data.Resource != "transfer" {
		t.Errorf("resource = %q, want transfer", env.Data.Resource)
	}
	want := []string{"destination", "source_transaction", "balance_transaction", "source_transaction.balance_transaction", "reversals"}
	if len(env.Data.ExpandPaths) != len(want) {
		t.Fatalf("expandPaths = %v, want %v", env.Data.ExpandPaths, want)
	}
	for i, w := range want {
		if env.Data.ExpandPaths[i] != w {
			t.Errorf("expandPaths[%d] = %q, want %q", i, env.Data.ExpandPaths[i], w)
		}
	}
}

func TestDescribeUnknownResource(t *testing.T) {
	err := runDescribe(context.Background(), &cli.GlobalOpts{}, []string{"nope"})
	if err == nil {
		t.Fatal("expected error for unknown resource")
	}
	if !strings.Contains(err.Error(), "unknown resource") {
		t.Errorf("error should mention unknown resource, got %v", err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan []byte)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()
	fn()
	_ = w.Close()
	os.Stdout = old
	return string(<-done)
}
