package webhookendpoint

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/testutil"
)

func TestWebhookEndpointList_Smoke(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"we_1","object":"webhook_endpoint","enabled_events":["charge.succeeded"]}],"has_more":false,"url":"/v1/webhook_endpoints"}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), opts, []string{}); err != nil {
		t.Fatalf("runList: %v", err)
	}
}

func TestWebhookEndpointGet_Smoke(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"we_1","object":"webhook_endpoint","enabled_events":["*"]}`)
	}))
	defer srv.Close()
	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runGet(context.Background(), opts, []string{"we_1"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
}

const fixtureEndpointsBody = `{"object":"list","data":[` +
	`{"id":"we_match","object":"webhook_endpoint","url":"https://a.example/hook","enabled_events":["charge.succeeded","customer.created"]},` +
	`{"id":"we_wildcard","object":"webhook_endpoint","url":"https://b.example/hook","enabled_events":["*"]},` +
	`{"id":"we_other","object":"webhook_endpoint","url":"https://c.example/hook","enabled_events":["invoice.paid"]}` +
	`],"has_more":false,"url":"/v1/webhook_endpoints"}`

func TestWebhookEndpoint_ForEvent_LiteralType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/webhook_endpoints") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, fixtureEndpointsBody)
	}))
	defer srv.Close()

	env := runForEventCapturing(t, srv.URL, "charge.succeeded")
	ids := pluckIDs(t, env)
	if !contains(ids, "we_match") || !contains(ids, "we_wildcard") {
		t.Errorf("expected we_match and we_wildcard, got %v", ids)
	}
	if contains(ids, "we_other") {
		t.Errorf("we_other should not match, got %v", ids)
	}
}

func TestWebhookEndpoint_ForEvent_EventIDFetchesEventFirst(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.Contains(r.URL.Path, "/v1/events/evt_123") {
			_, _ = io.WriteString(w, `{"id":"evt_123","object":"event","type":"charge.succeeded"}`)
			return
		}
		_, _ = io.WriteString(w, fixtureEndpointsBody)
	}))
	defer srv.Close()

	env := runForEventCapturing(t, srv.URL, "evt_123")

	gotEvent, gotEndpoints := false, false
	for _, p := range paths {
		if strings.Contains(p, "/v1/events/evt_123") {
			gotEvent = true
		}
		if strings.HasSuffix(p, "/v1/webhook_endpoints") {
			gotEndpoints = true
		}
	}
	if !gotEvent || !gotEndpoints {
		t.Errorf("expected event + endpoints calls, got paths=%v", paths)
	}
	ids := pluckIDs(t, env)
	if !contains(ids, "we_match") || !contains(ids, "we_wildcard") {
		t.Errorf("expected we_match and we_wildcard, got %v", ids)
	}
}

// runForEventCapturing invokes runForEvent and returns the parsed JSON
// envelope written to stdout.
func runForEventCapturing(t *testing.T, baseURL, arg string) map[string]any {
	t.Helper()
	opts := testutil.NewOpts(baseURL)
	body := captureStdoutFor(t, opts, []string{arg})
	var env map[string]any
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v\nbody: %s", err, body)
	}
	return env
}

func captureStdoutFor(t *testing.T, opts *cli.GlobalOpts, args []string) []byte {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	if err := runForEvent(context.Background(), opts, args); err != nil {
		_ = w.Close()
		os.Stdout = old
		t.Fatalf("runForEvent: %v", err)
	}
	_ = w.Close()
	os.Stdout = old
	return <-done
}

func pluckIDs(t *testing.T, env map[string]any) []string {
	t.Helper()
	data, ok := env["data"].([]any)
	if !ok {
		t.Fatalf("envelope.data not an array: %T", env["data"])
	}
	var ids []string
	for _, item := range data {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := m["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
