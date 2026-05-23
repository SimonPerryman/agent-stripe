// Package stripe wraps the Stripe SDK to enforce read-only HTTP access,
// pin the API version, and add account-aware client construction.
package stripe

import (
	"errors"
	"net/http"
)

// ErrReadOnly is returned by the read-only transport when a non-GET request
// reaches the HTTP boundary. The CLI never constructs writes, so this should
// only ever trigger from a bug or future code that forgot the contract.
var ErrReadOnly = errors.New("agent-stripe is read-only: only GET requests are permitted")

// readOnlyTransport rejects any HTTP method other than GET. It is the single
// chokepoint — even if a command package wires up a write helper by mistake,
// the request never leaves the process.
type readOnlyTransport struct {
	inner http.RoundTripper
}

// NewReadOnlyTransport wraps t (or http.DefaultTransport if nil) so that
// non-GET requests fail before hitting the network.
func NewReadOnlyTransport(t http.RoundTripper) http.RoundTripper {
	if t == nil {
		t = http.DefaultTransport
	}
	return &readOnlyTransport{inner: t}
}

func (rt *readOnlyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return nil, ErrReadOnly
	}
	return rt.inner.RoundTrip(req)
}
