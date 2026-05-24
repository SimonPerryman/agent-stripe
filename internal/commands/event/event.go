// Package event implements `agent-stripe event ...`. The killer feature here
// is `event list --related <id>` — a client-side filter over recent events
// matching `data.object.id`, the core debugging tool for an agent trying to
// reconstruct what happened to a Stripe object.
package event

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/output"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// DefaultMaxScan caps how many events `--related` will look through before
// giving up. 500 is enough to reconstruct recent activity without burning
// pagination round-trips; the response includes `scan.truncated` so the agent
// knows to narrow the window if it cares.
const DefaultMaxScan = 500

const Usage = `event — read Stripe events (the agent's debugging tool)

Subcommands:
  list [--type T] [--created-gt T] [--created-lt T] [--limit N] [--related ID]
                                            List recent events. --related filters
                                            client-side by data.object.id; useful
                                            for "what happened to this object?".
                                            With --stream + --related: emits header
                                            line, one event per line, then a final
                                            {"_truncated":bool,"scanned":N,"matched":M}
                                            line. --max-scan still applies;
                                            --limit caps matched count if set.

Note: Stripe does not offer a Search API for events — use list with
--type / --created-gt/lt / --related filters.

Streaming: pass --stream (top-level) on list to emit NDJSON over pages until
--limit or exhausted. With --related, see the --related-specific behavior above.`

func Run(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	switch args[0] {
	case "list":
		return runList(ctx, opts, args[1:])
	case "usage", "help":
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	return fmt.Errorf("unknown event subcommand %q", args[0])
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("event list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	typ := fs.String("type", "", "filter by event type (e.g. customer.created, charge.*)")
	createdGT := fs.Int64("created-gt", 0, "filter: created > unix seconds")
	createdLT := fs.Int64("created-lt", 0, "filter: created < unix seconds")
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return")
	related := fs.String("related", "", "client-side filter: only events whose data.object.id matches")
	maxScan := fs.Int("max-scan", DefaultMaxScan, "max events to scan when --related is set")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return errors.New("usage: event list [flags] (no positional args)")
	}

	params := &stripeapi.EventListParams{}
	params.Limit = stripeapi.Int64(100) // page size; cap is on items returned
	if *typ != "" {
		params.Type = stripeapi.String(*typ)
	}
	if *createdGT > 0 || *createdLT > 0 {
		r := &stripeapi.RangeQueryParams{}
		if *createdGT > 0 {
			r.GreaterThan = *createdGT
		}
		if *createdLT > 0 {
			r.LesserThan = *createdLT
		}
		params.CreatedRange = r
	}

	if *related != "" {
		list := opts.Client.V1Events.List(ctx, params)
		if opts.Stream {
			return runRelatedStream(ctx, opts, list, *related, *maxScan, *limit, cli.LimitExplicit(fs))
		}
		return runRelated(ctx, opts, list, *related, *maxScan, *limit)
	}

	if opts.Stream {
		params.Limit = stripeapi.Int64(100)
		cap := 0
		if cli.LimitExplicit(fs) {
			cap = *limit
		}
		return cli.StreamList(ctx, opts, opts.Client.V1Events.List(ctx, params), cap)
	}

	list := opts.Client.V1Events.List(ctx, params)
	items, hasMore, nextCursor, err := agentstripe.CollectRawList(ctx, list, *limit)
	if err != nil {
		return err
	}
	rendered, err := output.Render(items, output.Options{Full: opts.Full, Expand: opts.Expand, ExpandPaths: opts.ExpandPaths})
	if err != nil {
		return err
	}
	return output.Emit(os.Stdout, output.Envelope{
		Mode:       string(opts.Account.Mode),
		Account:    opts.Account.Alias,
		APIVersion: agentstripe.PinnedAPIVersion,
		Data:       rendered,
		Page:       &output.Page{HasMore: hasMore, NextCursor: nextCursor, Count: len(items)},
	})
}

func runRelated(ctx context.Context, opts *cli.GlobalOpts, list *stripeapi.V1List[*stripeapi.Event], related string, maxScan, limit int) error {
	matched := make([]map[string]any, 0, 16)
	scanned := 0
	truncated := false

	for evt, iterErr := range list.All(ctx) {
		if iterErr != nil {
			return iterErr
		}
		scanned++
		m, err := agentstripe.ToRawMap(evt)
		if err != nil {
			return err
		}
		if eventMatchesObject(m, related) {
			matched = append(matched, m)
			if len(matched) >= limit {
				truncated = true
				break
			}
		}
		if scanned >= maxScan {
			truncated = true
			break
		}
	}

	rendered, err := output.Render(matched, output.Options{Full: opts.Full, Expand: opts.Expand, ExpandPaths: opts.ExpandPaths})
	if err != nil {
		return err
	}
	return output.Emit(os.Stdout, output.Envelope{
		Mode:       string(opts.Account.Mode),
		Account:    opts.Account.Alias,
		APIVersion: agentstripe.PinnedAPIVersion,
		Data:       rendered,
		Scan:       &output.Scan{Scanned: scanned, Matched: len(matched), Truncated: truncated},
	})
}

func runRelatedStream(ctx context.Context, opts *cli.GlobalOpts, list *stripeapi.V1List[*stripeapi.Event], related string, maxScan, limit int, limitExplicit bool) error {
	streamer, err := output.NewStreamer(os.Stdout, cli.EnvelopeFor(opts))
	if err != nil {
		if output.IsBrokenPipe(err) {
			return nil
		}
		return err
	}
	scanned, matched := 0, 0
	truncated := false
	for evt, iterErr := range list.All(ctx) {
		if iterErr != nil {
			return iterErr
		}
		scanned++
		m, err := agentstripe.ToRawMap(evt)
		if err != nil {
			return err
		}
		if eventMatchesObject(m, related) {
			rendered, rErr := cli.RenderForStream(m, opts)
			if rErr != nil {
				return rErr
			}
			if wErr := streamer.Write(rendered); wErr != nil {
				if output.IsBrokenPipe(wErr) {
					return nil
				}
				return wErr
			}
			matched++
			if limitExplicit && matched >= limit {
				truncated = true
				break
			}
		}
		if scanned >= maxScan {
			truncated = true
			break
		}
	}
	if err := streamer.WriteSummary(map[string]any{
		"_truncated": truncated,
		"scanned":    scanned,
		"matched":    matched,
	}); err != nil {
		if output.IsBrokenPipe(err) {
			return nil
		}
		return err
	}
	return nil
}

// eventMatchesObject extracts data.object.id from the event map and compares
// against the target id.
func eventMatchesObject(evt map[string]any, target string) bool {
	data, ok := evt["data"].(map[string]any)
	if !ok {
		return false
	}
	obj, ok := data["object"].(map[string]any)
	if !ok {
		return false
	}
	id, _ := obj["id"].(string)
	return id == target
}
