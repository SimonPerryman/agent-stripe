package invoice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/config"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"
)

func TestInvoiceList_StatusAndSubscriptionPassthrough(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/invoices"}`)
	}))
	defer srv.Close()

	opts := newOpts(srv.URL)
	restore := captureStdout(t)
	if err := runList(context.Background(), opts, []string{"--status", "paid", "--subscription", "sub_xxx"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	restore()

	if !strings.Contains(got, "status=paid") {
		t.Errorf("expected status=paid, got %q", got)
	}
	if !strings.Contains(got, "subscription=sub_xxx") {
		t.Errorf("expected subscription=sub_xxx, got %q", got)
	}
}

// Locks in §5: lines.data[].description follows the same path-agnostic
// truncation rule as every other string field — truncated by default,
// preserved under --full. If per-field path overrides land in Phase 4,
// this test gets revisited.
const longDescription = "Custom invoice line for prorated usage covering the period from " +
	"March 1 through May 24, including the upgrade from Basic to Pro on March 12, " +
	"the seat add-on purchased on April 3, and the discount applied from coupon " +
	"SPRING2026 that reduced the net amount by 15 percent."

func TestInvoiceGet_LineDescriptionTruncatedByDefault(t *testing.T) {
	if len(longDescription) <= 200 {
		t.Fatalf("fixture too short (%d chars)", len(longDescription))
	}
	body := `{"id":"in_1","object":"invoice","lines":{"object":"list","data":[{"id":"il_1","object":"line_item","description":` +
		jsonString(longDescription) + `}]}}`
	out := runInvoiceGet(t, body, false)

	desc := pluckLineDescription(t, out)
	if !strings.HasSuffix(desc, "…") {
		t.Errorf("expected truncated description ending in …, got %q (len %d)", desc, len(desc))
	}
}

func TestInvoiceGet_LineDescriptionPreservedWithFull(t *testing.T) {
	body := `{"id":"in_1","object":"invoice","lines":{"object":"list","data":[{"id":"il_1","object":"line_item","description":` +
		jsonString(longDescription) + `}]}}`
	out := runInvoiceGet(t, body, true)

	desc := pluckLineDescription(t, out)
	if desc != longDescription {
		t.Errorf("expected full description under --full, got %q", desc)
	}
}

func runInvoiceGet(t *testing.T, body string, full bool) map[string]any {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	opts := newOpts(srv.URL)
	opts.Full = full

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
	if runErr := runGet(context.Background(), opts, []string{"in_1"}); runErr != nil {
		_ = w.Close()
		os.Stdout = old
		t.Fatalf("runGet: %v", runErr)
	}
	_ = w.Close()
	os.Stdout = old
	res := <-done
	if res.err != nil {
		t.Fatal(res.err)
	}
	var env map[string]any
	if err := json.Unmarshal(res.data, &env); err != nil {
		t.Fatalf("decode envelope: %v\nbody: %s", err, res.data)
	}
	return env
}

func pluckLineDescription(t *testing.T, env map[string]any) string {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data map, got %T", env["data"])
	}
	lines, ok := data["lines"].(map[string]any)
	if !ok {
		t.Fatalf("expected lines map, got %T", data["lines"])
	}
	dataArr, ok := lines["data"].([]any)
	if !ok || len(dataArr) == 0 {
		t.Fatalf("expected lines.data array, got %T", lines["data"])
	}
	first, ok := dataArr[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first line as map, got %T", dataArr[0])
	}
	desc, _ := first["description"].(string)
	return desc
}

func TestInvoiceSearch_QueryAndPage(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"object":"search_result","data":[{"id":"in_1","object":"invoice"}],"has_more":false,"next_page":"tok_next","url":"/v1/invoices/search"}`)
	}))
	defer srv.Close()

	opts := newOpts(srv.URL)
	restore := captureStdout(t)
	if err := runSearch(context.Background(), opts, []string{"--query", `status:"paid"`, "--page", "tok_abc", "--limit", "5"}); err != nil {
		t.Fatalf("runSearch: %v", err)
	}
	restore()

	if !strings.Contains(gotQuery, "query=status%3A%22paid%22") {
		t.Errorf("expected query=... in querystring, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "page=tok_abc") {
		t.Errorf("expected page=tok_abc, got %q", gotQuery)
	}
}

func TestInvoiceSearch_MissingQueryErrors(t *testing.T) {
	opts := newOpts("http://unused")
	if err := runSearch(context.Background(), opts, []string{}); err == nil {
		t.Fatal("expected error when --query is missing")
	}
}

func newOpts(baseURL string) *cli.GlobalOpts {
	return &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", baseURL, 5*time.Second),
	}
}

func captureStdout(t *testing.T) func() {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	return func() {
		_ = w.Close()
		os.Stdout = old
		<-done
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
