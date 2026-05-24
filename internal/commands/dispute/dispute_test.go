package dispute

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

	"github.com/shhac/agent-stripe/internal/cli"
	"github.com/shhac/agent-stripe/internal/config"
	agentstripe "github.com/shhac/agent-stripe/internal/stripe"
)

// longText is over the 200-char default truncation cap so we can verify
// evidence.* gets truncated by default and preserved under --full.
const longText = "The customer received the goods on March 12 and signed for the package. " +
	"We have shipping confirmation, a signed receipt, and email correspondence " +
	"acknowledging successful delivery. The dispute appears to be the result of " +
	"buyer's remorse rather than non-delivery."

func TestDisputeGet_EvidenceTruncatedByDefault(t *testing.T) {
	if len(longText) <= 200 {
		t.Fatalf("fixture too short (%d chars); update longText", len(longText))
	}
	body := `{"id":"dp_1","object":"dispute","evidence":{"cancellation_rebuttal":` +
		jsonString(longText) + `}}`
	srv := newServer(body)
	defer srv.Close()

	out := runDisputeGet(t, srv.URL, "dp_1", false)

	evidence := pluck(t, out, "data", "evidence", "cancellation_rebuttal")
	got, _ := evidence.(string)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected truncated string ending in …, got %q (len %d)", got, len(got))
	}
	if len(got) > 201+len("…") {
		t.Errorf("truncated string longer than expected: %d", len(got))
	}
}

func TestDisputeGet_EvidencePreservedWithFull(t *testing.T) {
	body := `{"id":"dp_1","object":"dispute","evidence":{"cancellation_rebuttal":` +
		jsonString(longText) + `}}`
	srv := newServer(body)
	defer srv.Close()

	out := runDisputeGet(t, srv.URL, "dp_1", true)

	evidence := pluck(t, out, "data", "evidence", "cancellation_rebuttal")
	got, _ := evidence.(string)
	if got != longText {
		t.Errorf("expected full text under --full, got %q", got)
	}
}

func newServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
}

func runDisputeGet(t *testing.T, baseURL, id string, full bool) map[string]any {
	t.Helper()
	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", baseURL, 5*time.Second),
		Full:    full,
	}
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
	if runErr := runGet(context.Background(), opts, []string{id}); runErr != nil {
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

func pluck(t *testing.T, m map[string]any, keys ...string) any {
	t.Helper()
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("expected map at key %q, got %T (path so far: %v)", k, cur, keys)
		}
		cur = mm[k]
	}
	return cur
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
