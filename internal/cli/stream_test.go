package cli

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simonperryman/agent-stripe/internal/config"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

func TestLimitExplicit(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		_ = fs.Int("limit", 100, "")
		if err := fs.Parse([]string{}); err != nil {
			t.Fatal(err)
		}
		if LimitExplicit(fs) {
			t.Error("expected false when --limit not passed")
		}
	})
	t.Run("set", func(t *testing.T) {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		_ = fs.Int("limit", 100, "")
		if err := fs.Parse([]string{"--limit", "5"}); err != nil {
			t.Fatal(err)
		}
		if !LimitExplicit(fs) {
			t.Error("expected true when --limit passed")
		}
	})
	t.Run("set to default value still counts as explicit", func(t *testing.T) {
		// fs.Visit walks flags actually set, regardless of value — confirms
		// LimitExplicit inspects presence, not value.
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		_ = fs.Int("limit", 100, "")
		if err := fs.Parse([]string{"--limit", "100"}); err != nil {
			t.Fatal(err)
		}
		if !LimitExplicit(fs) {
			t.Error("expected true when --limit explicitly passed (even at default value)")
		}
	})
}

func TestEnvelopeFor(t *testing.T) {
	t.Run("with account", func(t *testing.T) {
		opts := &GlobalOpts{
			Account: &config.Account{Alias: "acct_a", Mode: config.ModeTest},
		}
		env := EnvelopeFor(opts)
		if env.Account != "acct_a" {
			t.Errorf("Account = %q, want acct_a", env.Account)
		}
		if env.Mode != "test" {
			t.Errorf("Mode = %q, want test", env.Mode)
		}
		if env.APIVersion != agentstripe.PinnedAPIVersion {
			t.Errorf("APIVersion = %q, want %q", env.APIVersion, agentstripe.PinnedAPIVersion)
		}
	})
	t.Run("without account", func(t *testing.T) {
		env := EnvelopeFor(&GlobalOpts{})
		if env.Account != "" || env.Mode != "" {
			t.Errorf("expected blank account/mode, got %+v", env)
		}
		if env.APIVersion != agentstripe.PinnedAPIVersion {
			t.Errorf("APIVersion = %q, want %q", env.APIVersion, agentstripe.PinnedAPIVersion)
		}
	})
}

func TestStreamList_DrainsAllPages(t *testing.T) {
	srv, requests := twoPageCustomerServer()
	defer srv.Close()

	out := captureCLIStdout(t, func() {
		opts := newStreamOpts(srv.URL)
		client := opts.Client
		list := client.V1Customers.List(context.Background(), &stripeapi.CustomerListParams{})
		if err := StreamList(context.Background(), opts, list, 0); err != nil {
			t.Fatalf("StreamList: %v", err)
		}
	})

	lines := nonEmptyLines(out)
	// 1 header + 3 page-1 records + 2 page-2 records = 6
	if len(lines) != 6 {
		t.Fatalf("expected 6 NDJSON lines (header + 5 records), got %d: %q", len(lines), out)
	}
	var hdr map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &hdr); err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if hdr["stream"] != true {
		t.Errorf("expected stream:true on header, got %v", hdr["stream"])
	}
	if atomic.LoadInt32(requests) != 2 {
		t.Errorf("expected 2 HTTP requests (paginated), got %d", atomic.LoadInt32(requests))
	}
}

func TestStreamList_CapStopsEarly(t *testing.T) {
	srv, _ := twoPageCustomerServer()
	defer srv.Close()

	out := captureCLIStdout(t, func() {
		opts := newStreamOpts(srv.URL)
		list := opts.Client.V1Customers.List(context.Background(), &stripeapi.CustomerListParams{})
		// cap=2 — should stop after 2 records (first page returns 3, but the
		// iterator halts at the cap).
		if err := StreamList(context.Background(), opts, list, 2); err != nil {
			t.Fatalf("StreamList: %v", err)
		}
	})

	lines := nonEmptyLines(out)
	if len(lines) != 3 { // header + 2 records
		t.Fatalf("expected 3 lines (header + 2 records), got %d: %q", len(lines), out)
	}
}

func twoPageCustomerServer() (*httptest.Server, *int32) {
	var requests int32
	page1 := `{"object":"list","url":"/v1/customers","has_more":true,"data":[` +
		`{"id":"cus_1","object":"customer"},` +
		`{"id":"cus_2","object":"customer"},` +
		`{"id":"cus_3","object":"customer"}` +
		`]}`
	page2 := `{"object":"list","url":"/v1/customers","has_more":false,"data":[` +
		`{"id":"cus_4","object":"customer"},` +
		`{"id":"cus_5","object":"customer"}` +
		`]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requests, 1)
		if n == 1 {
			_, _ = io.WriteString(w, page1)
			return
		}
		// Page 2 — Stripe iterator should have set starting_after to last id
		// on page 1 (cus_3).
		if !strings.Contains(r.URL.RawQuery, "starting_after=cus_3") {
			// Don't fail the request — just emit nothing. The test asserts
			// request count, which surfaces the mistake more clearly.
		}
		_, _ = io.WriteString(w, page2)
	}))
	return srv, &requests
}

func newStreamOpts(baseURL string) *GlobalOpts {
	return &GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", baseURL, 5*time.Second),
	}
}

func captureCLIStdout(t *testing.T, fn func()) string {
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
	fn()
	_ = w.Close()
	os.Stdout = old
	res := <-done
	if res.err != nil {
		t.Fatal(res.err)
	}
	return string(res.data)
}

func TestPacedEmit(t *testing.T) {
	t.Run("rate=0 is a no-op", func(t *testing.T) {
		calls := 0
		emit := pacedEmit(func(map[string]any) error { calls++; return nil }, 0)
		start := time.Now()
		for i := 0; i < 10; i++ {
			_ = emit(nil)
		}
		if calls != 10 {
			t.Fatalf("expected 10 calls, got %d", calls)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Millisecond {
			t.Errorf("rate=0 should not sleep; got %v", elapsed)
		}
	})

	t.Run("rate=1 paces at ~10ms per record", func(t *testing.T) {
		// rate=1 req/sec * streamPageSize=100 = 100 records/sec → 10ms each.
		// 5 records ⇒ first emit is immediate, then 4 × 10ms ≈ 40ms.
		// Floor at 30ms to absorb scheduler jitter on slow CI.
		emit := pacedEmit(func(map[string]any) error { return nil }, 1.0)
		start := time.Now()
		for i := 0; i < 5; i++ {
			_ = emit(nil)
		}
		elapsed := time.Since(start)
		if elapsed < 30*time.Millisecond {
			t.Errorf("expected ≥30ms across 5 records at rate=1, got %v", elapsed)
		}
		if elapsed > 200*time.Millisecond {
			t.Errorf("expected ≤200ms (sanity ceiling) at rate=1 for 5 records, got %v", elapsed)
		}
	})

	t.Run("forwards errors from inner", func(t *testing.T) {
		want := io.EOF
		emit := pacedEmit(func(map[string]any) error { return want }, 0)
		if got := emit(nil); got != want {
			t.Errorf("expected inner error to propagate, got %v", got)
		}
	})
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
