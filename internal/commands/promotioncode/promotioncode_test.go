package promotioncode

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/testutil"
)

func TestPromotionCodeGet(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"id":"promo_1","object":"promotion_code","code":"LAUNCH20","active":true}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	if err := runGet(context.Background(), opts, []string{"promo_1"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
	if gotPath != "/v1/promotion_codes/promo_1" {
		t.Errorf("path = %q, want /v1/promotion_codes/promo_1", gotPath)
	}
}

func TestPromotionCodeGet_RequiresID(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	if err := runGet(context.Background(), opts, nil); err == nil {
		t.Fatal("expected error when no id")
	}
}

func TestPromotionCodeList_FiltersPassthrough(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.URL.Path != "/v1/promotion_codes" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/promotion_codes"}`)
	}))
	defer srv.Close()

	opts := testutil.NewOpts(srv.URL)
	testutil.CaptureStdout(t)
	args := []string{
		"--code", "LAUNCH20",
		"--coupon", "cpn_1",
		"--customer", "cus_1",
		"--created-gt", "10",
		"--starting-after", "promo_x",
		"--limit", "5",
	}
	if err := runList(context.Background(), opts, args); err != nil {
		t.Fatalf("runList: %v", err)
	}
	for _, want := range []string{"code=LAUNCH20", "coupon=cpn_1", "customer=cus_1", "created[gt]=10", "starting_after=promo_x", "limit=5"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("expected %q in query, got %q", want, gotQuery)
		}
	}
	// Neither --active nor --inactive was passed, so the tri-state filter must
	// stay off the wire entirely rather than defaulting to active=false.
	if strings.Contains(gotQuery, "active=") {
		t.Errorf("expected no active filter when neither flag is passed, got %q", gotQuery)
	}
}

func TestPromotionCodeList_ActiveTriState(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"active", []string{"--active"}, "active=true"},
		{"inactive", []string{"--inactive"}, "active=false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotQuery string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.RawQuery
				_, _ = io.WriteString(w, `{"object":"list","data":[],"has_more":false,"url":"/v1/promotion_codes"}`)
			}))
			defer srv.Close()

			opts := testutil.NewOpts(srv.URL)
			testutil.CaptureStdout(t)
			if err := runList(context.Background(), opts, tc.args); err != nil {
				t.Fatalf("runList: %v", err)
			}
			if !strings.Contains(gotQuery, tc.want) {
				t.Errorf("expected %q in query, got %q", tc.want, gotQuery)
			}
		})
	}
}

func TestPromotionCodeList_RejectsBothActiveFlags(t *testing.T) {
	opts := testutil.NewOpts("http://unused")
	if err := runList(context.Background(), opts, []string{"--active", "--inactive"}); err == nil {
		t.Fatal("expected error when both --active and --inactive are passed")
	}
}

func TestPromotionCodeRun_Dispatch(t *testing.T) {
	opts := &cli.GlobalOpts{}
	if err := Run(context.Background(), opts, nil); err != nil {
		t.Errorf("empty: %v", err)
	}
	if err := Run(context.Background(), opts, []string{"usage"}); err != nil {
		t.Errorf("usage: %v", err)
	}
	if err := Run(context.Background(), opts, []string{"help"}); err != nil {
		t.Errorf("help: %v", err)
	}
	if err := Run(context.Background(), opts, []string{"nope"}); err == nil {
		t.Error("expected error for unknown subcommand")
	}
}
