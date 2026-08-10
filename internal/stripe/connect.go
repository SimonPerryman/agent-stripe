package stripe

import "net/http"

// StripeAccountHeader is the request header that scopes an API call to a
// connected account. Stripe has no separate Connect API — the same endpoints
// read a different account's books when this header is present.
const StripeAccountHeader = "Stripe-Account"

// stripeAccountTransport stamps the Stripe-Account header on every outbound
// request. Like readOnlyTransport, it is a single chokepoint: every command
// package inherits Connect support without touching its params structs.
//
// stripe-go/v85 exposes the header only as Params.StripeAccount (per-request),
// with no client-level option — so wiring it here is the only way to make it
// impossible for a new command to forget.
type stripeAccountTransport struct {
	account string
	inner   http.RoundTripper
}

// NewStripeAccountTransport wraps t (or http.DefaultTransport if nil) so every
// request carries Stripe-Account: account. An empty account returns t
// unchanged, so the platform-scoped path allocates nothing extra.
func NewStripeAccountTransport(account string, t http.RoundTripper) http.RoundTripper {
	if t == nil {
		t = http.DefaultTransport
	}
	if account == "" {
		return t
	}
	return &stripeAccountTransport{account: account, inner: t}
}

func (rt *stripeAccountTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Never overwrite a header the caller already set — a future per-params
	// override should win over the global flag.
	if req.Header.Get(StripeAccountHeader) == "" {
		// Clone before mutating: RoundTrippers must not modify the request
		// they are given (net/http contract).
		req = req.Clone(req.Context())
		req.Header.Set(StripeAccountHeader, rt.account)
	}
	return rt.inner.RoundTrip(req)
}
