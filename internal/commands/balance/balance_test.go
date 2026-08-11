package balance

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/config"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"
)

func TestBalanceGet_EnvelopeHasObjectData_NoPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/balance" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"object":"balance","available":[{"amount":1000,"currency":"usd"}],"pending":[{"amount":200,"currency":"usd"}],"livemode":false}`)
	}))
	defer srv.Close()

	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", srv.URL, "", 5*time.Second),
	}
	env := captureEnvelope(t, func() error {
		return runGet(context.Background(), opts, nil)
	})

	if _, hasPage := env["page"]; hasPage {
		t.Error("expected no page sibling on balance get")
	}
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be an object, got %T", env["data"])
	}
	if _, ok := data["available"]; !ok {
		t.Errorf("expected data.available, got keys: %v", keys(data))
	}
}

func captureEnvelope(t *testing.T, run func() error) map[string]any {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	type res struct {
		b   []byte
		err error
	}
	done := make(chan res, 1)
	go func() {
		b, e := io.ReadAll(r)
		done <- res{b, e}
	}()
	if runErr := run(); runErr != nil {
		_ = w.Close()
		os.Stdout = old
		t.Fatalf("run: %v", runErr)
	}
	_ = w.Close()
	os.Stdout = old
	r2 := <-done
	if r2.err != nil {
		t.Fatal(r2.err)
	}
	var env map[string]any
	if err := json.Unmarshal(r2.b, &env); err != nil {
		t.Fatalf("decode envelope: %v\nbody: %s", err, r2.b)
	}
	return env
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestBalanceTransactions_FiltersPassthrough(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.URL.Path != "/v1/balance_transactions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/balance_transactions"}`)
	}))
	defer srv.Close()

	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", srv.URL, "", 5*time.Second),
	}
	old := os.Stdout
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	os.Stdout = devnull
	defer func() { os.Stdout = old; _ = devnull.Close() }()

	args := []string{
		"--type", "charge",
		"--payout", "po_1",
		"--currency", "usd",
		"--source", "ch_1",
		"--created-gt", "100",
		"--created-lt", "200",
		"--starting-after", "txn_x",
		"--limit", "5",
	}
	if err := runTransactions(context.Background(), opts, args); err != nil {
		t.Fatalf("runTransactions: %v", err)
	}
	for _, want := range []string{"type=charge", "payout=po_1", "currency=usd", "source=ch_1", "created[gt]=100", "created[lt]=200", "starting_after=txn_x", "limit=5"} {
		if !contains(gotQuery, want) {
			t.Errorf("expected %q in query, got %q", want, gotQuery)
		}
	}
}

func TestBalanceTransaction_GetsOneRow(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"id":"txn_1","object":"balance_transaction","amount":1000,"fee":59,"net":941,"currency":"usd"}`)
	}))
	defer srv.Close()

	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", srv.URL, "", 5*time.Second),
	}
	env := captureEnvelope(t, func() error {
		return runTransaction(context.Background(), opts, []string{"txn_1"})
	})

	if gotPath != "/v1/balance_transactions/txn_1" {
		t.Errorf("path = %q, want /v1/balance_transactions/txn_1", gotPath)
	}
	if _, hasPage := env["page"]; hasPage {
		t.Error("expected no page sibling on a single-object read")
	}
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be an object, got %T", env["data"])
	}
	if data["id"] != "txn_1" {
		t.Errorf("data.id = %v, want txn_1", data["id"])
	}
}

func TestBalanceTransaction_RejectsWrongArgCount(t *testing.T) {
	opts := &cli.GlobalOpts{Account: &config.Account{Alias: "test", Mode: config.ModeTest}}
	for _, args := range [][]string{nil, {"txn_1", "txn_2"}} {
		if err := runTransaction(context.Background(), opts, args); err == nil {
			t.Errorf("args %v: expected error", args)
		}
	}
}

func TestBalanceGet_RejectsArgs(t *testing.T) {
	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
	}
	if err := runGet(context.Background(), opts, []string{"extra"}); err == nil {
		t.Fatal("expected error when args provided to balance get")
	}
}

func TestBalanceRun_Dispatch(t *testing.T) {
	opts := &cli.GlobalOpts{Account: &config.Account{Alias: "test", Mode: config.ModeTest}}
	if err := Run(context.Background(), opts, nil); err != nil {
		t.Errorf("empty args: %v", err)
	}
	if err := Run(context.Background(), opts, []string{"usage"}); err != nil {
		t.Errorf("usage: %v", err)
	}
	if err := Run(context.Background(), opts, []string{"help"}); err != nil {
		t.Errorf("help: %v", err)
	}
	if err := Run(context.Background(), opts, []string{"nope"}); err == nil {
		t.Error("expected error for unknown subcommand")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
