package stripe

import (
	"net/http"
	"time"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// PinnedAPIVersion is the Stripe API version the CLI is built against.
// stripe-go/v85 already pins this; we re-export it so callers can include
// it in the response envelope without importing the SDK directly.
const PinnedAPIVersion = stripeapi.APIVersion

// NewClient returns a Stripe client configured for read-only access against
// the given URL (or the Stripe default if baseURL is empty), with the given
// per-request timeout. The HTTP transport is wrapped to reject non-GET calls.
func NewClient(apiKey, baseURL string, timeout time.Duration) *stripeapi.Client {
	httpClient := &http.Client{
		Transport: NewReadOnlyTransport(nil),
		Timeout:   timeout,
	}
	cfg := &stripeapi.BackendConfig{
		HTTPClient: httpClient,
	}
	if baseURL != "" {
		cfg.URL = stripeapi.String(baseURL)
	}
	backend := stripeapi.GetBackendWithConfig(stripeapi.APIBackend, cfg)
	return stripeapi.NewClient(apiKey, stripeapi.WithBackends(&stripeapi.Backends{
		API:         backend,
		Connect:     backend,
		Uploads:     backend,
		MeterEvents: backend,
	}))
}
