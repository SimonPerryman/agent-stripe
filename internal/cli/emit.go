package cli

import (
	"os"

	"github.com/simonperryman/agent-stripe/internal/output"
)

// EmitSingle renders one Stripe object as a single-envelope JSON response.
// Replaces the 5-line Render/Emit tail that every `get` command used to inline.
func EmitSingle(opts *GlobalOpts, m map[string]any) error {
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

func renderOpts(opts *GlobalOpts) output.Options {
	return output.Options{Full: opts.Full, Expand: opts.Expand, ExpandPaths: opts.ExpandPaths}
}
