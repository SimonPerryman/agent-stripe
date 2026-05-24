package output

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestStreamerHeaderShape(t *testing.T) {
	var buf bytes.Buffer
	s, err := NewStreamer(&buf, Envelope{Mode: "test", Account: "acct_a", APIVersion: "2025-01-27"})
	if err != nil {
		t.Fatal(err)
	}
	// Header should be the first line; data/page/scan absent.
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 header line, got %d (%q)", len(lines), buf.String())
	}
	var hdr map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &hdr); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if hdr["stream"] != true {
		t.Errorf("expected stream:true on header, got %v", hdr["stream"])
	}
	if _, ok := hdr["data"]; ok {
		t.Errorf("header must not contain data, got %v", hdr["data"])
	}
	if _, ok := hdr["page"]; ok {
		t.Errorf("header must not contain page")
	}
	_ = s
}

func TestStreamerWritesOneRecordPerLine(t *testing.T) {
	var buf bytes.Buffer
	s, err := NewStreamer(&buf, Envelope{Mode: "test", Account: "a", APIVersion: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []map[string]any{{"id": "ch_1"}, {"id": "ch_2"}} {
		if err := s.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 { // 1 header + 2 records
		t.Fatalf("expected 3 lines (header + 2 records), got %d: %q", len(lines), buf.String())
	}
	var rec1 map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &rec1); err != nil {
		t.Fatalf("unmarshal record 1: %v", err)
	}
	if rec1["id"] != "ch_1" {
		t.Errorf("record 1 id wrong: %v", rec1)
	}
	// Records must not be wrapped in {"data": ...} — they're bare objects.
	if _, ok := rec1["data"]; ok {
		t.Error("record line should not have a data wrapper")
	}
}

func TestStreamerBrokenPipeOnHeader(t *testing.T) {
	// io.Pipe writer-side returns broken-pipe-ish error when reader is closed.
	r, w := io.Pipe()
	_ = r.Close()
	_, err := NewStreamer(w, Envelope{Mode: "test", Account: "a", APIVersion: "v1"})
	if err == nil {
		t.Fatal("expected error writing header to closed reader")
	}
	// We won't always get our errBrokenPipe sentinel from io.Pipe — its error is
	// io.ErrClosedPipe. The point of the test is just that header writes don't
	// panic and return a non-nil err in this case.
}
