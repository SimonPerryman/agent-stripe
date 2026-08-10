package cli

import (
	"os"

	"github.com/simonperryman/agent-stripe/internal/output"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"
)

// EmitSingle renders one Stripe object as a single-envelope JSON response.
// Replaces the 5-line Render/Emit tail that every `get` command used to inline.
//
// It takes the SDK value rather than a map so the struct-vs-raw decoding
// choice lives here, once, instead of at 26 call sites where a new command
// could quietly forget --raw and answer it with struct-marshalled output.
func EmitSingle(opts *GlobalOpts, obj any) error {
	m, err := RawMap(opts, obj)
	if err != nil {
		return err
	}
	rendered, err := output.Render(m, renderOpts(opts))
	if err != nil {
		return err
	}
	env := EnvelopeFor(opts)
	env.Data = rendered
	return output.Emit(os.Stdout, env)
}

// EmitList renders a paginated list as a single envelope with Page metadata.
func EmitList(opts *GlobalOpts, items []map[string]any, hasMore bool, nextCursor string) error {
	rendered, err := output.Render(items, renderOpts(opts))
	if err != nil {
		return err
	}
	env := EnvelopeFor(opts)
	env.Data = rendered
	env.Page = &output.Page{HasMore: hasMore, NextCursor: nextCursor, Count: len(items)}
	return output.Emit(os.Stdout, env)
}

// RawMap converts an SDK value to the map the renderer walks, honouring
// --raw. Commands that need the map before emitting (to filter it, or to
// project a nested list out of it) use this rather than calling the stripe
// package directly, so they cannot get the mode wrong.
func RawMap(opts *GlobalOpts, obj any) (map[string]any, error) {
	return agentstripe.ToRawMap(obj, opts.Raw)
}

func renderOpts(opts *GlobalOpts) output.Options {
	return output.Options{Full: opts.Full, Expand: opts.Expand, ExpandPaths: opts.ExpandPaths}
}
