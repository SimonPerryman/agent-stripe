package resource

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/output"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// defaultDepth caps reflective recursion. Customer.Address is depth 1;
// Subscription.Items.Data[].Price.Product is depth 4. 3 keeps the common case
// readable without burying the agent in nested noise.
const defaultDepth = 3

// resourceRegistry maps the CLI resource name to a zero-value struct from the
// SDK. Reflection happens on the type, never on an instance — no API call,
// no Stripe round-trip.
var resourceRegistry = map[string]any{
	"customer":              stripeapi.Customer{},
	"charge":                stripeapi.Charge{},
	"payment-intent":        stripeapi.PaymentIntent{},
	"payment-method":        stripeapi.PaymentMethod{},
	"setup-intent":          stripeapi.SetupIntent{},
	"setup-attempt":         stripeapi.SetupAttempt{},
	"refund":                stripeapi.Refund{},
	"dispute":               stripeapi.Dispute{},
	"balance":               stripeapi.BalanceTransaction{},
	"payout":                stripeapi.Payout{},
	"transfer":              stripeapi.Transfer{},
	"event":                 stripeapi.Event{},
	"checkout-session":      stripeapi.CheckoutSession{},
	"subscription":          stripeapi.Subscription{},
	"subscription-item":     stripeapi.SubscriptionItem{},
	"subscription-schedule": stripeapi.SubscriptionSchedule{},
	"invoice":               stripeapi.Invoice{},
	"invoice-item":          stripeapi.InvoiceItem{},
	"invoice-line-item":     stripeapi.InvoiceLineItem{},
	"product":               stripeapi.Product{},
	"price":                 stripeapi.Price{},
	"webhook-endpoint":      stripeapi.WebhookEndpoint{},
	"connected-account":     stripeapi.Account{},
	"person":                stripeapi.Person{},
	"capability":            stripeapi.Capability{},
	"application-fee":       stripeapi.ApplicationFee{},
	"fee-refund":            stripeapi.FeeRefund{},
}

// expandPathsByResource mirrors the "Recommended --expand-stripe paths"
// curation each command's Usage block calls out. Kept in sync by hand — these
// are opinions, not reflections.
//
// Every path here is checked against the pinned SDK's response structs by
// TestExpandPathsResolveAgainstSDKStructs. That matters because Stripe will
// happily accept an expand path for a field the SDK no longer models, return
// the data, and let our marshalling drop it — the caller sees null with no
// error. Do not add a path the test cannot resolve.
//
// On *list* endpoints these paths need a "data." prefix (Stripe expands
// relative to the list wrapper): `--expand-stripe data.customer`, not
// `customer`.
var expandPathsByResource = map[string][]string{
	"customer":              {"default_source", "subscriptions"},
	"charge":                {"customer", "balance_transaction", "application_fee", "transfer", "source_transfer", "on_behalf_of"},
	"payment-intent":        {"latest_charge", "customer", "payment_method", "latest_charge.balance_transaction"},
	"refund":                {"charge", "balance_transaction", "payment_intent"},
	"dispute":               {"charge", "payment_intent", "balance_transactions"},
	"payout":                {"destination", "balance_transaction"},
	"transfer":              {"destination", "source_transaction", "balance_transaction", "source_transaction.balance_transaction", "reversals"},
	"event":                 {},
	"balance":               {},
	"subscription":          {"customer", "latest_invoice", "default_payment_method", "items.data.price.product", "pending_setup_intent"},
	"subscription-item":     {"price.product"},
	"subscription-schedule": {"subscription", "customer", "phases.items.price.product"},
	"invoice":               {"customer", "parent.subscription_details.subscription", "lines.data.pricing.price_details.price"},
	"invoice-item":          {"customer", "invoice", "pricing.price_details.price"},
	"invoice-line-item":     {"pricing.price_details.price"},
	"product":               {"default_price"},
	"price":                 {"product"},
	"payment-method":        {"customer"},
	"setup-intent":          {"customer", "payment_method", "latest_attempt"},
	"setup-attempt":         {"payment_method"},
	"checkout-session":      {"payment_intent", "subscription", "setup_intent", "customer", "line_items"},
	"webhook-endpoint":      {},
	"connected-account":     {"external_accounts", "settings", "requirements"},
	"application-fee":       {"charge", "balance_transaction", "refunds", "originating_transaction"},
	// person / capability / fee-refund are small, self-contained shapes with
	// nothing worth expanding.
	"person":     {},
	"capability": {},
	"fee-refund": {},
}

// lowSignal is the housekeeping field set we mark `lowSignal: true` on so the
// agent can scroll past `object`, `livemode`, etc. without ignoring them.
var lowSignal = map[string]struct{}{
	"object":   {},
	"livemode": {},
}

// fieldInfo is the leaf record emitted by describe — one per struct field.
type fieldInfo struct {
	Field     string      `json:"field"`
	Type      string      `json:"type"`
	Repeated  bool        `json:"repeated,omitempty"` // slice or array
	Nullable  bool        `json:"nullable,omitempty"` // pointer type or omitempty
	LowSignal bool        `json:"lowSignal,omitempty"`
	Fields    []fieldInfo `json:"fields,omitempty"` // populated for nested structs (within depth)
}

func runDescribe(_ context.Context, opts *cli.GlobalOpts, args []string) error {
	// Accept either order: `describe customer --depth 2` or `describe --depth 2 customer`.
	// Pull the first non-flag arg out as the resource name, parse the rest.
	if len(args) == 0 {
		return errors.New("usage: resource describe <name> [--depth N]")
	}
	fs := flag.NewFlagSet("resource describe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	depth := fs.Int("depth", defaultDepth, "max field-tree depth (default 3)")
	if err := fs.Parse(cli.ReorderFlagsFirst(args, fs)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: resource describe <name> [--depth N]")
	}
	name := fs.Arg(0)
	z, ok := resourceRegistry[name]
	if !ok {
		known := knownResources()
		hint := ""
		if match := output.Closest(name, known); match != "" {
			hint = fmt.Sprintf("did you mean %q?", match)
		} else {
			hint = output.ValidList(known)
		}
		return &output.Error{
			Msg:  fmt.Sprintf("unknown resource %q (try one of: %s)", name, strings.Join(known, ", ")),
			Hint: hint,
			By:   output.FixableByAgent,
		}
	}
	tree := walkType(reflect.TypeOf(z), *depth, 0)
	data := map[string]any{
		"resource":    name,
		"fields":      tree,
		"expandPaths": expandPathsByResource[name],
	}
	return output.Emit(os.Stdout, output.Envelope{
		Mode:       safeMode(opts),
		Account:    safeAccount(opts),
		APIVersion: agentstripe.PinnedAPIVersion,
		Data:       data,
	})
}

func knownResources() []string {
	out := make([]string, 0, len(resourceRegistry))
	for k := range resourceRegistry {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// walkType reflects over a struct type, returning one fieldInfo per exported
// field with a json tag. Pointers are unwrapped (and marked Nullable);
// slices/arrays are descended once (with Repeated=true). depth bounds total
// recursion; current is the current level (root = 0).
func walkType(t reflect.Type, depth, current int) []fieldInfo {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	out := make([]fieldInfo, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		jsonTag := f.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		name, omitempty := parseJSONTag(jsonTag)
		fi := fieldInfo{
			Field:    name,
			Type:     typeLabel(f.Type),
			Nullable: f.Type.Kind() == reflect.Pointer || omitempty,
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array {
			fi.Repeated = true
			ft = ft.Elem()
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
		}
		if _, ok := lowSignal[name]; ok {
			fi.LowSignal = true
		}
		if ft.Kind() == reflect.Struct && current+1 < depth {
			fi.Fields = walkType(ft, depth, current+1)
		}
		out = append(out, fi)
	}
	return out
}

func parseJSONTag(tag string) (name string, omitempty bool) {
	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty
}

// typeLabel returns a short, agent-friendly type name (e.g. "string",
// "*Customer", "[]*Item"). Full package paths are dropped — they're noise.
func typeLabel(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return "*" + typeLabel(t.Elem())
	case reflect.Slice, reflect.Array:
		return "[]" + typeLabel(t.Elem())
	case reflect.Map:
		return "map[" + typeLabel(t.Key()) + "]" + typeLabel(t.Elem())
	}
	name := t.Name()
	if name == "" {
		return t.Kind().String()
	}
	return name
}

func safeMode(opts *cli.GlobalOpts) string {
	if opts == nil || opts.Account == nil {
		return ""
	}
	return string(opts.Account.Mode)
}

func safeAccount(opts *cli.GlobalOpts) string {
	if opts == nil || opts.Account == nil {
		return ""
	}
	return opts.Account.Alias
}
