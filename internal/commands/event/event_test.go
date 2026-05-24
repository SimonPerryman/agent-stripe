package event

import (
	"context"
	"encoding/json"
	"fmt"
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

// fakeEventsServer returns N synthetic events split across pages, with each
// event's data.object.id equal to objectIDFor(i). Stripe-style pagination via
// starting_after on the event id.
func fakeEventsServer(t *testing.T, total int, objectIDFor func(i int) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			fmt.Sscanf(v, "%d", &limit)
		}
		start := 0
		if sa := r.URL.Query().Get("starting_after"); sa != "" {
			// event ids are evt_<i>; parse to int
			var idx int
			fmt.Sscanf(sa, "evt_%d", &idx)
			start = idx + 1
		}
		end := start + limit
		if end > total {
			end = total
		}
		items := make([]map[string]any, 0, end-start)
		for i := start; i < end; i++ {
			items = append(items, map[string]any{
				"id":     fmt.Sprintf("evt_%d", i),
				"object": "event",
				"type":   "customer.created",
				"data": map[string]any{
					"object": map[string]any{
						"id":     objectIDFor(i),
						"object": "customer",
					},
				},
			})
		}
		resp := map[string]any{
			"object":   "list",
			"data":     items,
			"has_more": end < total,
			"url":      "/v1/events",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func newEventOpts(baseURL string, stream bool) *cli.GlobalOpts {
	return &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", baseURL, 5*time.Second),
		Stream:  stream,
	}
}

// captureStdoutLines redirects os.Stdout, runs fn, then returns the captured
// stdout split into trimmed lines.
func captureStdoutLines(t *testing.T, fn func()) []string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	doneCh := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		doneCh <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stdout = old
	out := <-doneCh
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func parseLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("unmarshal %q: %v", line, err)
	}
	return m
}

func TestRelatedStream_ScanBudgetExhausted(t *testing.T) {
	srv := fakeEventsServer(t, 600, func(i int) string { return fmt.Sprintf("cus_other_%d", i) })
	defer srv.Close()

	opts := newEventOpts(srv.URL, true)
	var err error
	lines := captureStdoutLines(t, func() {
		err = runList(context.Background(), opts, []string{"--related", "cus_target", "--max-scan", "100"})
	})
	if err != nil {
		t.Fatalf("runList: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header + summary), got %d: %v", len(lines), lines)
	}
	hdr := parseLine(t, lines[0])
	if hdr["stream"] != true {
		t.Errorf("header missing stream:true: %v", hdr)
	}
	if _, ok := hdr["data"]; ok {
		t.Errorf("header should not have data: %v", hdr)
	}
	summary := parseLine(t, lines[1])
	if summary["_truncated"] != true {
		t.Errorf("expected _truncated:true, got %v", summary)
	}
	if summary["scanned"].(float64) != 100 {
		t.Errorf("expected scanned:100, got %v", summary["scanned"])
	}
	if summary["matched"].(float64) != 0 {
		t.Errorf("expected matched:0, got %v", summary["matched"])
	}
}

func TestRelatedStream_AllMatchedWithinBudget(t *testing.T) {
	matchAt := map[int]bool{5: true, 17: true, 42: true}
	srv := fakeEventsServer(t, 50, func(i int) string {
		if matchAt[i] {
			return "cus_target"
		}
		return fmt.Sprintf("cus_other_%d", i)
	})
	defer srv.Close()

	opts := newEventOpts(srv.URL, true)
	var err error
	lines := captureStdoutLines(t, func() {
		err = runList(context.Background(), opts, []string{"--related", "cus_target", "--max-scan", "500"})
	})
	if err != nil {
		t.Fatalf("runList: %v", err)
	}
	if len(lines) != 5 { // header + 3 records + summary
		t.Fatalf("expected 5 lines, got %d: %v", len(lines), lines)
	}
	// Records: lines 1..3 should each have a top-level id (event records).
	for i := 1; i <= 3; i++ {
		rec := parseLine(t, lines[i])
		if _, ok := rec["id"].(string); !ok {
			t.Errorf("record line %d missing id: %v", i, rec)
		}
	}
	summary := parseLine(t, lines[4])
	if summary["_truncated"] != false {
		t.Errorf("expected _truncated:false, got %v", summary)
	}
	if summary["scanned"].(float64) != 50 {
		t.Errorf("expected scanned:50, got %v", summary["scanned"])
	}
	if summary["matched"].(float64) != 3 {
		t.Errorf("expected matched:3, got %v", summary["matched"])
	}
	// Summary line has no top-level id — that's the signal agents use to skip it.
	if _, ok := summary["id"]; ok {
		t.Errorf("summary line should not have id: %v", summary)
	}
}

func TestRelatedStream_LimitHardStop(t *testing.T) {
	srv := fakeEventsServer(t, 200, func(i int) string { return "cus_target" })
	defer srv.Close()

	opts := newEventOpts(srv.URL, true)
	var err error
	lines := captureStdoutLines(t, func() {
		err = runList(context.Background(), opts, []string{"--related", "cus_target", "--limit", "5"})
	})
	if err != nil {
		t.Fatalf("runList: %v", err)
	}
	if len(lines) != 7 { // header + 5 records + summary
		t.Fatalf("expected 7 lines, got %d: %v", len(lines), lines)
	}
	summary := parseLine(t, lines[6])
	if summary["_truncated"] != true {
		t.Errorf("expected _truncated:true, got %v", summary)
	}
	if summary["scanned"].(float64) != 5 {
		t.Errorf("expected scanned:5 (loop exits as soon as matched hits limit), got %v", summary["scanned"])
	}
	if summary["matched"].(float64) != 5 {
		t.Errorf("expected matched:5, got %v", summary["matched"])
	}
}

func TestRelatedStream_BrokenPipeNoSummary(t *testing.T) {
	srv := fakeEventsServer(t, 200, func(i int) string { return "cus_target" })
	defer srv.Close()

	opts := newEventOpts(srv.URL, true)

	// Swap os.Stdout for an io.Pipe whose reader we control: read the header
	// then close, so subsequent record writes hit a broken pipe.
	old := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = pw
	defer func() { os.Stdout = old }()

	// Read the header line, then close the reader.
	captured := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 4096)
		// Read once for the header.
		n, _ := pr.Read(buf)
		head := append([]byte(nil), buf[:n]...)
		_ = pr.Close()
		// Drain anything else (shouldn't be much; closed reader will surface broken pipe on writer).
		_, _ = io.Copy(io.Discard, pr)
		captured <- head
	}()

	runErr := runList(context.Background(), opts, []string{"--related", "cus_target"})
	_ = pw.Close()
	head := <-captured

	if runErr != nil {
		t.Fatalf("runList: expected nil on broken pipe, got %v", runErr)
	}
	// Captured bytes should be exactly the header — no summary line afterwards.
	// (The reader was closed right after one Read, before the loop could write a summary.)
	lines := strings.Split(strings.TrimRight(string(head), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected only header captured before close, got %d lines: %v", len(lines), lines)
	}
	hdr := parseLine(t, lines[0])
	if hdr["stream"] != true {
		t.Errorf("expected header with stream:true, got %v", hdr)
	}
}
