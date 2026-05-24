package cli

import (
	"context"
	"flag"
	"os"
	"time"

	"github.com/simonperryman/agent-stripe/internal/output"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// streamPageSize is the page size every list/search command pins under
// --stream. Pacing is calibrated against this: at rate=R requests/sec,
// each record gets a sleep of 1s / (R * streamPageSize), so the implied
// HTTP-request rate stays at R. If a command ever changes its page size
// under --stream, update this constant too or the pacing will be wrong.
const streamPageSize = 100

// LimitExplicit reports whether the user explicitly set --limit on fs. Used by
// list/search commands to decide whether --limit is a hard stop (under
// --stream) or a default-bounded cap.
func LimitExplicit(fs *flag.FlagSet) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "limit" {
			set = true
		}
	})
	return set
}

// EnvelopeFor builds the envelope header for a resource list/search response,
// based on the resolved account on opts.
func EnvelopeFor(opts *GlobalOpts) output.Envelope {
	mode := ""
	alias := ""
	if opts.Account != nil {
		mode = string(opts.Account.Mode)
		alias = opts.Account.Alias
	}
	return output.Envelope{
		Mode:       mode,
		Account:    alias,
		APIVersion: agentstripe.PinnedAPIVersion,
	}
}

// RenderForStream renders a single record under the global render options.
// Identical to the non-stream path; pulled out so each command's stream loop
// is one line.
func RenderForStream(rec map[string]any, opts *GlobalOpts) (any, error) {
	return output.Render(rec, output.Options{
		Full:        opts.Full,
		Expand:      opts.Expand,
		ExpandPaths: opts.ExpandPaths,
	})
}

// StreamList drains a *V1List[T] to os.Stdout as NDJSON: one envelope header
// then one rendered record per line. cap > 0 caps the total emitted (a hard
// stop, used when the user passed --limit explicitly under --stream). Broken
// pipe (e.g. `| head`) returns nil for a clean exit.
func StreamList[T any](ctx context.Context, opts *GlobalOpts, list *stripeapi.V1List[T], cap int) error {
	return streamIter(ctx, opts, cap, func(emit func(map[string]any) error) (int, error) {
		return agentstripe.StreamRawList(ctx, list, cap, emit)
	})
}

// StreamSearch is the V1SearchList[T] counterpart of StreamList.
func StreamSearch[T any](ctx context.Context, opts *GlobalOpts, list *stripeapi.V1SearchList[T], cap int) error {
	return streamIter(ctx, opts, cap, func(emit func(map[string]any) error) (int, error) {
		return agentstripe.StreamRawSearch(ctx, list, cap, emit)
	})
}

func streamIter(_ context.Context, opts *GlobalOpts, _ int, drain func(emit func(map[string]any) error) (int, error)) error {
	streamer, err := output.NewStreamer(os.Stdout, EnvelopeFor(opts))
	if err != nil {
		if output.IsBrokenPipe(err) {
			return nil
		}
		return err
	}
	emit := pacedEmit(func(rec map[string]any) error {
		rendered, rErr := RenderForStream(rec, opts)
		if rErr != nil {
			return rErr
		}
		return streamer.Write(rendered)
	}, opts.RateLimit)
	_, err = drain(emit)
	if output.IsBrokenPipe(err) {
		return nil
	}
	return err
}

// pacedEmit wraps inner so that calls are spaced at most rate*streamPageSize
// per second — i.e. the implied HTTP-request rate stays at `rate` req/sec.
// rate <= 0 disables pacing. The pacer never accumulates debt: if upstream
// stalls for longer than one page-interval, the next call fires immediately
// and the clock resets from there.
func pacedEmit(inner func(map[string]any) error, rate float64) func(map[string]any) error {
	if rate <= 0 {
		return inner
	}
	interval := time.Duration(float64(time.Second) / (rate * streamPageSize))
	var next time.Time
	return func(rec map[string]any) error {
		now := time.Now()
		if next.IsZero() {
			next = now.Add(interval)
		} else if now.Before(next) {
			time.Sleep(next.Sub(now))
			next = next.Add(interval)
		} else {
			next = now.Add(interval)
		}
		return inner(rec)
	}
}
