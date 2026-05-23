package stripe

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadOnlyTransportAllowsGET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	c := &http.Client{Transport: NewReadOnlyTransport(nil)}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET should pass: %v", err)
	}
	resp.Body.Close()
}

func TestReadOnlyTransportRejectsWrites(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("write should never reach the server, method=%s", r.Method)
	}))
	defer srv.Close()
	c := &http.Client{Transport: NewReadOnlyTransport(nil)}
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		req, _ := http.NewRequest(method, srv.URL, strings.NewReader(""))
		_, err := c.Do(req)
		if !errors.Is(err, ErrReadOnly) {
			// http.Client wraps errors; check via the wrapper.
			if err == nil || !strings.Contains(err.Error(), ErrReadOnly.Error()) {
				t.Fatalf("%s: expected ErrReadOnly, got %v", method, err)
			}
		}
	}
}
