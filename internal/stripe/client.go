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

// ClientOption customizes the client returned by NewClient. Options exist for
// settings that are absent on almost every call — folding them into the
// signature would put an empty string at ~70 call sites and invite the two
// adjacent string arguments to be swapped.
type ClientOption func(*clientConfig)

type clientConfig struct {
	apiVersion string
}

// WithAPIVersion overrides the Stripe-Version header the SDK would otherwise
// send (PinnedAPIVersion). Empty is a no-op. Callers that set this should
// also request raw output — the response is shaped by the named version, but
// the SDK's structs still only model the pinned one, so the typed path would
// drop precisely the fields the override was asked for.
func WithAPIVersion(v string) ClientOption {
	return func(c *clientConfig) { c.apiVersion = v }
}

// NewClient returns a Stripe client configured for read-only access against
// the given URL (or the Stripe default if baseURL is empty), with the given
// per-request timeout. The HTTP transport is wrapped to reject non-GET calls.
//
// stripeAccount, when non-empty, scopes every request to that connected
// account via the Stripe-Account header. The read-only transport stays
// outermost so its guarantee is evaluated before anything else in the chain.
func NewClient(apiKey, baseURL, stripeAccount string, timeout time.Duration, options ...ClientOption) *stripeapi.Client {
	var cc clientConfig
	for _, opt := range options {
		opt(&cc)
	}
	httpClient := &http.Client{
		Transport: NewReadOnlyTransport(
			NewStripeAccountTransport(stripeAccount,
				NewAPIVersionTransport(cc.apiVersion, nil)),
		),
		Timeout: timeout,
	}
	cfg := &stripeapi.BackendConfig{
		HTTPClient: httpClient,
		// Silence the SDK's own ERROR-level logger — every 4xx/5xx response
		// gets logged to stderr by default, which leaks past the single-line
		// JSON error envelope agents parse. We surface Stripe errors through
		// output.FailFromStripeError instead.
		LeveledLogger: &stripeapi.LeveledLogger{Level: stripeapi.LevelNull},
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
