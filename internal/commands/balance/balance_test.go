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
		Client:  agentstripe.NewClient("sk_test_fake", srv.URL, 5*time.Second),
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
