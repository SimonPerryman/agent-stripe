// Package webhookendpoint implements `agent-stripe webhook-endpoint ...`.
package webhookendpoint

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/simonperryman/agent-stripe/internal/cli"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

const Usage = `webhook-endpoint — read configured Stripe webhook endpoints

Subcommands:
  get <id>                                  Fetch one endpoint (we_...)
  list [--limit N] [--starting-after WE]    List configured endpoints
  for-event <evt_id_or_event_type>          List endpoints whose enabled_events
                                            would match this event

for-event accepts either an event id (evt_...) — in which case the event is
fetched first to resolve event.type (2 GETs) — or a literal event type
(e.g. charge.succeeded). Endpoints with enabled_events containing "*" or the
resolved type are returned with the same envelope shape as 'list'.

Webhook *delivery attempts* are not exposed by the Stripe API (Dashboard only).
For delivery-failure debugging, use the 'event' command — events carry
pending_webhooks and request metadata.

No Search API for webhook endpoints.

Streaming: pass --stream (top-level) on list to emit NDJSON over pages until
--limit or exhausted. for-event drains all endpoints (typically single-digit
counts) into a single envelope.

Connect: an endpoint with a non-null 'application' (ca_...) is a Connect
endpoint — it receives events for every account connected to that application.
A null 'application' means an account endpoint, receiving only this account's
own events. That field is the reliable signal; do not infer it from the URL.
Stripe's 'connect' parameter is create/list-only and is absent from the
response, so it cannot be read back.

A connected account can also register endpoints of its own, which the platform
cannot see: pass --stripe-account acct_... (global) to list those.

Each endpoint also carries its own 'api_version', which is the version its
consumer actually receives — frequently pinned years behind, and independent
of the apiVersion this CLI reports. When an endpoint's payload disagrees with
what you read here, compare those two first.

Help: usage | help | -h | --help`

func Run(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	switch args[0] {
	case "get":
		return runGet(ctx, opts, args[1:])
	case "list":
		return runList(ctx, opts, args[1:])
	case "for-event":
		return runForEvent(ctx, opts, args[1:])
	case "usage", "help":
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	return fmt.Errorf("unknown webhook-endpoint subcommand %q", args[0])
}

func runGet(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: webhook-endpoint get <id>")
	}
	params := &stripeapi.WebhookEndpointRetrieveParams{}
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	we, err := opts.Client.V1WebhookEndpoints.Retrieve(ctx, args[0], params)
	if err != nil {
		return err
	}
	m, err := agentstripe.ToRawMap(we)
	if err != nil {
		return err
	}
	return cli.EmitSingle(opts, m)
}

func runList(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	fs := flag.NewFlagSet("webhook-endpoint list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", agentstripe.DefaultMaxResults, "max items to return (cap)")
	startingAfter := fs.String("starting-after", "", "cursor: we_... id from previous page")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params := &stripeapi.WebhookEndpointListParams{}
	params.Limit = stripeapi.Int64(int64(min(*limit, agentstripe.MaxPageSize)))
	params.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	if *startingAfter != "" {
		params.StartingAfter = stripeapi.String(*startingAfter)
	}
	if opts.Stream {
		params.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	}
	return cli.RunListOrStream(ctx, opts, opts.Client.V1WebhookEndpoints.List(ctx, params), *limit, cli.LimitExplicit(fs))
}

func runForEvent(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: webhook-endpoint for-event <evt_id_or_event_type>")
	}
	arg := args[0]
	eventType := arg
	if strings.HasPrefix(arg, "evt_") {
		ev, err := opts.Client.V1Events.Retrieve(ctx, arg, &stripeapi.EventRetrieveParams{})
		if err != nil {
			return err
		}
		eventType = string(ev.Type)
		if eventType == "" {
			return fmt.Errorf("event %s has no type field", arg)
		}
	}

	listParams := &stripeapi.WebhookEndpointListParams{}
	listParams.Limit = stripeapi.Int64(agentstripe.MaxPageSize)
	listParams.Expand = agentstripe.ExpandSlice(opts.ExpandStripe)
	endpoints, _, _, err := agentstripe.CollectRawList(ctx, opts.Client.V1WebhookEndpoints.List(ctx, listParams), agentstripe.MaxPageSize)
	if err != nil {
		return err
	}

	matched := make([]map[string]any, 0, len(endpoints))
	for _, ep := range endpoints {
		if matchesEvent(ep, eventType) {
			matched = append(matched, ep)
		}
	}
	return cli.EmitList(opts, matched, false, "")
}

func matchesEvent(endpoint map[string]any, eventType string) bool {
	raw, ok := endpoint["enabled_events"].([]any)
	if !ok {
		return false
	}
	for _, e := range raw {
		s, ok := e.(string)
		if !ok {
			continue
		}
		if s == "*" || s == eventType {
			return true
		}
	}
	return false
}
