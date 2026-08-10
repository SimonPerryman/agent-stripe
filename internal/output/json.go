// Package output handles the JSON response envelope, truncation, pruning,
// and streaming. All command output flows through here so the envelope shape
// (`{mode, account, apiVersion, data}`) is uniform across the CLI.
package output

import (
	"encoding/json"
	"errors"
	"io"
	"syscall"
)

// DefaultTruncateLength is the cap on string field length before truncation.
const DefaultTruncateLength = 200

// Envelope is the wrapper around every successful response.
type Envelope struct {
	Mode    string `json:"mode"`
	Account string `json:"account"`
	// StripeAccount echoes the connected account these results were read
	// from (--stripe-account). Empty — and omitted — means the platform
	// account. Without it a platform charge and a connected-account charge
	// are indistinguishable in the output.
	StripeAccount string `json:"stripeAccount,omitempty"`
	// APIVersion is the version these results were *requested* at, not the
	// one the CLI is built against. Under --api-version the two differ, and
	// echoing the pinned constant regardless would make the field a lie
	// exactly when a reader most needs it.
	APIVersion string `json:"apiVersion"`
	// Raw marks output decoded from Stripe's response body rather than the
	// SDK's structs (--raw, and implied by --api-version). It is a different
	// contract: every wire field survives, but none of the shape guarantees
	// the typed path offers do.
	Raw    bool  `json:"raw,omitempty"`
	Data   any   `json:"data,omitempty"`
	Page   *Page `json:"page,omitempty"`
	Scan   *Scan `json:"scan,omitempty"`
	Stream bool  `json:"stream,omitempty"`
}

// Page describes pagination state for list/search responses.
type Page struct {
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor,omitempty"`
	Count      int    `json:"count"`
}

// Scan describes client-side filter coverage (event list --related).
type Scan struct {
	Scanned   int  `json:"scanned"`
	Matched   int  `json:"matched"`
	Truncated bool `json:"truncated"`
}

// Options control how Data is rendered (truncation, expansion, full).
type Options struct {
	Full   bool
	Expand []string // bare field names — matched as leaf names anywhere in the tree
	// ExpandPaths is dotted paths like "lines.data.description" — matched against
	// the full path from the root of Data. More precise than Expand: lets an
	// agent skip truncation on one specific field without globbing every
	// same-named leaf in the tree.
	ExpandPaths    []string
	TruncateLength int // 0 = DefaultTruncateLength
}

// Render applies the options to data: walks the structure, prunes empties,
// truncates long strings unless --full or the field is in --expand.
// The data must be JSON-serializable; for arbitrary types we round-trip
// through JSON to convert into a uniform map/slice tree. Callers that already
// hold a map[string]any or []map[string]any (every list/get command does, via
// agentstripe.ToRawMap) hit a fast path that skips the redundant round-trip.
func Render(data any, opts Options) (any, error) {
	if data == nil {
		return nil, nil
	}
	tl := opts.TruncateLength
	if tl == 0 {
		tl = DefaultTruncateLength
	}
	expandSet := make(map[string]struct{}, len(opts.Expand))
	for _, f := range opts.Expand {
		expandSet[f] = struct{}{}
	}
	pathSet := make(map[string]struct{}, len(opts.ExpandPaths))
	for _, p := range opts.ExpandPaths {
		pathSet[p] = struct{}{}
	}

	// Fast path: caller is already handing us a map / slice-of-maps. Skip the
	// marshal/unmarshal round-trip. walk only ever reads child references, so
	// the input map is not mutated.
	switch v := data.(type) {
	case map[string]any:
		return walk(v, "", "", opts.Full, expandSet, pathSet, tl), nil
	case []map[string]any:
		items := make([]any, len(v))
		for i, m := range v {
			items[i] = m
		}
		return walk(items, "", "", opts.Full, expandSet, pathSet, tl), nil
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	return walk(tree, "", "", opts.Full, expandSet, pathSet, tl), nil
}

// walk recurses through the decoded JSON tree. `key` is the immediate map key
// (leaf name) and `path` is the dotted path from the root, used for
// ExpandPaths matching. Slice indexes are not included in the path — agents
// pass `lines.data.description`, not `lines.data.0.description`.
func walk(node any, key, path string, full bool, expand, expandPaths map[string]struct{}, maxLen int) any {
	switch v := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			processed := walk(child, k, childPath, full, expand, expandPaths, maxLen)
			if isEmpty(processed) {
				continue
			}
			out[k] = processed
			if s, ok := processed.(string); ok && !full {
				if !shouldExpand(k, childPath, expand, expandPaths) {
					if orig, ok := child.(string); ok && len(orig) > maxLen && len(s) <= maxLen+len("…") {
						out[k+"Length"] = len(orig)
					}
				}
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, child := range v {
			// Path does not include the slice index — keep ExpandPaths
			// matching positional-agnostic.
			out = append(out, walk(child, key, path, full, expand, expandPaths, maxLen))
		}
		return out
	case string:
		if full {
			return v
		}
		if shouldExpand(key, path, expand, expandPaths) {
			return v
		}
		if len(v) > maxLen {
			return v[:maxLen] + "…"
		}
		return v
	default:
		return v
	}
}

func shouldExpand(leaf, path string, expand, expandPaths map[string]struct{}) bool {
	if _, ok := expand[leaf]; ok {
		return true
	}
	if path != "" {
		if _, ok := expandPaths[path]; ok {
			return true
		}
	}
	return false
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// Emit writes an envelope as a single JSON line.
func Emit(w io.Writer, env Envelope) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(env); err != nil {
		if isWriteEPIPE(err) {
			return nil
		}
		return err
	}
	return nil
}

// Streamer emits one envelope header line, then one data record per call.
type Streamer struct {
	w   io.Writer
	enc *json.Encoder
	hdr Envelope
}

// NewStreamer writes the header line (with stream:true, no data) and returns
// a streamer that emits one JSON object per Write call.
func NewStreamer(w io.Writer, hdr Envelope) (*Streamer, error) {
	hdr.Stream = true
	hdr.Data = nil
	hdr.Page = nil
	hdr.Scan = nil
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(hdr); err != nil {
		if isWriteEPIPE(err) {
			return nil, errBrokenPipe
		}
		return nil, err
	}
	return &Streamer{w: w, enc: enc, hdr: hdr}, nil
}

// Write emits a single record line. Returns errBrokenPipe if the reader
// closed the pipe; callers should treat that as a clean exit.
func (s *Streamer) Write(record any) error {
	if err := s.enc.Encode(record); err != nil {
		if isWriteEPIPE(err) {
			return errBrokenPipe
		}
		return err
	}
	if f, ok := s.w.(interface{ Sync() error }); ok {
		_ = f.Sync()
	}
	return nil
}

// WriteSummary emits a single trailing object — used by commands whose stream
// has a tail with shape distinct from the records (e.g. event --related's
// scan summary). Same broken-pipe semantics as Write. Caller is responsible
// for calling it at most once.
func (s *Streamer) WriteSummary(summary any) error {
	return s.Write(summary)
}

var errBrokenPipe = errors.New("broken pipe")

// IsBrokenPipe reports whether err is the sentinel signalling the consumer
// closed its end of the pipe (e.g. `| head`). Callers should exit 0.
func IsBrokenPipe(err error) bool {
	return errors.Is(err, errBrokenPipe)
}

// isWriteEPIPE reports whether err is the OS-level EPIPE raised by writing
// to a closed pipe (e.g. downstream `| head` exited). Distinct from the
// exported IsBrokenPipe, which checks for our own errBrokenPipe sentinel.
func isWriteEPIPE(err error) bool {
	return errors.Is(err, syscall.EPIPE)
}
