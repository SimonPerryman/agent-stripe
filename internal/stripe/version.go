package stripe

import "net/http"

// APIVersionHeader is the request header that selects which Stripe API
// version shapes the response. Stripe pins one per account, per webhook
// endpoint, and per request — the three can and do disagree.
const APIVersionHeader = "Stripe-Version"

// apiVersionTransport replaces the Stripe-Version header the SDK stamps on
// every request. Like the read-only and Stripe-Account transports it is a
// single chokepoint, so no command package can be built that forgets it.
//
// It differs from stripeAccountTransport in one way that matters: that one
// leaves an existing header alone, because a header already present must have
// been set deliberately by a caller. Here the header is *always* already
// present — stripe-go adds Stripe-Version: PinnedAPIVersion itself — so
// "don't overwrite" would make the override a no-op. Set, not Add.
type apiVersionTransport struct {
	version string
	inner   http.RoundTripper
}

// NewAPIVersionTransport wraps t (or http.DefaultTransport if nil) so every
// request carries Stripe-Version: version. An empty version returns t
// unchanged, leaving the SDK's pinned value in place.
func NewAPIVersionTransport(version string, t http.RoundTripper) http.RoundTripper {
	if t == nil {
		t = http.DefaultTransport
	}
	if version == "" {
		return t
	}
	return &apiVersionTransport{version: version, inner: t}
}

func (rt *apiVersionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone before mutating: RoundTrippers must not modify the request they
	// are given (net/http contract).
	req = req.Clone(req.Context())
	req.Header.Set(APIVersionHeader, rt.version)
	return rt.inner.RoundTrip(req)
}
