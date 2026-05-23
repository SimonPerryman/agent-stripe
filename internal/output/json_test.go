package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderTruncatesLongStrings(t *testing.T) {
	long := strings.Repeat("x", DefaultTruncateLength+50)
	in := map[string]any{"description": long, "id": "cus_1"}
	out, err := Render(in, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	s := m["description"].(string)
	if len(s) > DefaultTruncateLength+5 { // +ellipsis
		t.Fatalf("expected truncated string, got len %d", len(s))
	}
	if !strings.HasSuffix(s, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", s)
	}
	if got := m["descriptionLength"]; got == nil {
		t.Fatalf("expected descriptionLength companion")
	}
}

func TestRenderFullSkipsTruncation(t *testing.T) {
	long := strings.Repeat("x", DefaultTruncateLength+50)
	in := map[string]any{"description": long}
	out, _ := Render(in, Options{Full: true})
	m := out.(map[string]any)
	if len(m["description"].(string)) != DefaultTruncateLength+50 {
		t.Fatalf("expected full string under --full")
	}
	if _, ok := m["descriptionLength"]; ok {
		t.Fatalf("descriptionLength should not appear under --full")
	}
}

func TestRenderExpandSkipsField(t *testing.T) {
	long := strings.Repeat("x", DefaultTruncateLength+50)
	in := map[string]any{"description": long, "notes": long}
	out, _ := Render(in, Options{Expand: []string{"description"}})
	m := out.(map[string]any)
	if len(m["description"].(string)) != DefaultTruncateLength+50 {
		t.Fatalf("expected expand to skip truncation")
	}
	if !strings.HasSuffix(m["notes"].(string), "…") {
		t.Fatalf("expected non-expanded field to truncate")
	}
}

func TestRenderPrunesEmpty(t *testing.T) {
	in := map[string]any{"id": "cus_1", "name": nil, "email": "", "metadata": map[string]any{}}
	out, _ := Render(in, Options{})
	m := out.(map[string]any)
	if _, ok := m["name"]; ok {
		t.Fatalf("nil should be pruned")
	}
	if _, ok := m["email"]; ok {
		t.Fatalf("empty string should be pruned")
	}
	if _, ok := m["metadata"]; ok {
		t.Fatalf("empty map should be pruned")
	}
}

func TestEmitEnvelope(t *testing.T) {
	var buf bytes.Buffer
	env := Envelope{Mode: "test", Account: "acme", APIVersion: "2026-04-22.dahlia", Data: map[string]any{"id": "cus_1"}}
	if err := Emit(&buf, env); err != nil {
		t.Fatal(err)
	}
	var got Envelope
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != "test" || got.Account != "acme" {
		t.Fatalf("envelope round-trip mismatch: %+v", got)
	}
}

func TestStreamerHeaderThenRecords(t *testing.T) {
	var buf bytes.Buffer
	s, err := NewStreamer(&buf, Envelope{Mode: "test", Account: "acme", APIVersion: "v"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Write(map[string]any{"id": "a"})
	_ = s.Write(map[string]any{"id": "b"})
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	var hdr Envelope
	if err := json.Unmarshal([]byte(lines[0]), &hdr); err != nil {
		t.Fatal(err)
	}
	if !hdr.Stream || hdr.Data != nil {
		t.Fatalf("header line should be stream:true with no data")
	}
}
