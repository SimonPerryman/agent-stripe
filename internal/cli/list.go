package cli

import (
	"context"

	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// RunListOrStream dispatches a typed *V1List[T]: if --stream is set, drains to
// NDJSON via StreamList; otherwise collects up to limit items and emits a
// single envelope with Page metadata. limitExplicit is required so the stream
// branch knows whether --limit should be a hard cap.
//
// Callers must set params.Limit to 100 before constructing the iterator when
// --stream is in effect, because the page size is fixed under streaming.
func RunListOrStream[T any](ctx context.Context, opts *GlobalOpts, list *stripeapi.V1List[T], limit int, limitExplicit bool) error {
	if opts.Stream {
		cap := 0
		if limitExplicit {
			cap = limit
		}
		return StreamList(ctx, opts, list, cap)
	}
	items, hasMore, nextCursor, err := agentstripe.CollectRawList(ctx, list, limit, opts.Raw)
	if err != nil {
		return err
	}
	return EmitList(opts, items, hasMore, nextCursor)
}

// RunSearchOrStream is the *V1SearchList[T] counterpart of RunListOrStream.
func RunSearchOrStream[T any](ctx context.Context, opts *GlobalOpts, list *stripeapi.V1SearchList[T], limit int, limitExplicit bool) error {
	if opts.Stream {
		cap := 0
		if limitExplicit {
			cap = limit
		}
		return StreamSearch(ctx, opts, list, cap)
	}
	items, hasMore, nextCursor, err := agentstripe.CollectRawSearch(ctx, list, limit, opts.Raw)
	if err != nil {
		return err
	}
	return EmitList(opts, items, hasMore, nextCursor)
}
