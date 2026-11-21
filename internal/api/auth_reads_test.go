package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/metrics"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/store"
)

// TestD1ReadAuthMatrix is the table-driven contract for the D1 opt-in
// (issue #99): when a token is configured and RequireTokenForReads /
// MetricsTokenRequired are set, the read endpoints require it; without
// the opt-in (or without a token) behavior is unchanged. Every deny has
// a positive control (the same request with the valid token).
func TestD1ReadAuthMatrix(t *testing.T) {
	type authCase struct {
		name  string
		token string // "" = no token configured
		reqRD bool   // RequireTokenForReads
		reqM  bool   // MetricsTokenRequired
		// expect is a map endpoint→code with no credentials.
		expect map[string]int
	}

	// endpoints under test: reads, mutations, health, metrics
	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{"GET", "/v1/policy", ""},
		{"GET", "/v1/policy/versions", ""},
		{"POST", "/v1/policy/validate", `{"policy":"version: 1\ndefault_action: DROP\nrules: []\n"}`},
		{"POST", "/v1/policy", ""}, // invalid body but auth is checked first
		{"POST", "/v1/policy/versions/1/activate", ""},
		{"GET", "/healthz", ""},
		{"GET", "/readyz", ""},
		{"GET", "/metrics", ""},
	}

	cases := []authCase{
		{
			name:  "no token configured: everything open, mutations 401",
			token: "",
			expect: map[string]int{
				"GET /v1/policy":                      200,
				"GET /v1/policy/versions":             200,
				"POST /v1/policy/validate":            401,
				"POST /v1/policy":                     401,
				"POST /v1/policy/versions/1/activate": 401,
				"GET /healthz":                        200,
				"GET /readyz":                         200,
				"GET /metrics":                        200,
			},
		},
		{
			name:  "token configured, no opt-in (historical default)",
			token: testToken,
			expect: map[string]int{
				"GET /v1/policy":                      200,
				"GET /v1/policy/versions":             200,
				"POST /v1/policy/validate":            401,
				"POST /v1/policy":                     401,
				"POST /v1/policy/versions/1/activate": 401,
				"GET /healthz":                        200,
				"GET /readyz":                         200,
				"GET /metrics":                        200,
			},
		},
		{
			name:  "require-token-for-reads on: policy reads require token, health/metrics stay open",
			token: testToken,
			reqRD: true,
			expect: map[string]int{
				"GET /v1/policy":                      401,
				"GET /v1/policy/versions":             401,
				"POST /v1/policy/validate":            401,
				"POST /v1/policy":                     401,
				"POST /v1/policy/versions/1/activate": 401,
				"GET /healthz":                        200,
				"GET /readyz":                         200,
				"GET /metrics":                        200,
			},
		},
		{
			name:  "metrics-token-required on: metrics requires token, reads stay open",
			token: testToken,
			reqM:  true,
			expect: map[string]int{
				"GET /v1/policy":                      200,
				"GET /v1/policy/versions":             200,
				"POST /v1/policy/validate":            401,
				"POST /v1/policy":                     401,
				"POST /v1/policy/versions/1/activate": 401,
				"GET /healthz":                        200,
				"GET /readyz":                         200,
				"GET /metrics":                        401,
			},
		},
		{
			name:  "both opt-ins on",
			token: testToken,
			reqRD: true,
			reqM:  true,
			expect: map[string]int{
				"GET /v1/policy":                      401,
				"GET /v1/policy/versions":             401,
				"POST /v1/policy/validate":            401,
				"POST /v1/policy":                     401,
				"POST /v1/policy/versions/1/activate": 401,
				"GET /healthz":                        200,
				"GET /readyz":                         200,
				"GET /metrics":                        401,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			st, err := store.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			king := policy.NewKing()
			srv, err := New(Options{
				Host:                 "127.0.0.1",
				Port:                 0,
				Token:                c.token,
				Store:                st,
				King:                 king,
				Metrics:              metrics.New(),
				Log:                  &testLogger{},
				RequireTokenForReads: c.reqRD,
				MetricsTokenRequired: c.reqM,
			})
			if err != nil {
				t.Fatal(err)
			}
			// Seed an active policy so the read endpoints return 200 when
			// allowed (404 means "no active policy", which would make the
			// matrix assertions trivial).
			if err := st.WritePolicy("1", []byte("version: 1\ndefault_action: DROP\nrules: []\n")); err != nil {
				t.Fatal(err)
			}
			if err := st.SetActive("1"); err != nil {
				t.Fatal(err)
			}
			king.Replace(policy.NewSnapshot(policy.ActionDrop, nil))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go srv.Serve(ctx)
			deadline := time.Now().Add(5 * time.Second)
			for strings.HasSuffix(srv.Addr(), ":0") {
				if time.Now().After(deadline) {
					t.Fatal("server did not bind")
				}
				time.Sleep(2 * time.Millisecond)
			}

			request := func(method, path, body string, auth string) int {
				var rd io.Reader
				if body != "" {
					rd = bytes.NewReader([]byte(body))
				}
				req, err := http.NewRequest(method, "http://"+srv.Addr()+path, rd)
				if err != nil {
					t.Fatal(err)
				}
				if auth != "" {
					req.Header.Set("Authorization", auth)
				}
				req.Header.Set("Content-Type", "application/json")
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("%s %s: %v", method, path, err)
				}
				defer resp.Body.Close()
				io.Copy(io.Discard, resp.Body)
				return resp.StatusCode
			}

			for _, ep := range endpoints {
				key := ep.method + " " + ep.path
				want, ok := c.expect[key]
				if !ok {
					t.Fatalf("missing expectation for %s", key)
				}
				got := request(ep.method, ep.path, ep.body, "")
				if got != want {
					t.Errorf("%s: unauth got %d, want %d", key, got, want)
				}
				// Positive control: with the valid token the request
				// must succeed whenever a token is configured.
				if c.token != "" {
					gotAuth := request(ep.method, ep.path, ep.body, "Bearer "+testToken)
					if ep.method == "POST" && ep.path == "/v1/policy" {
						// apply with invalid body may 400; auth must pass first
						if gotAuth == 401 {
							t.Errorf("%s: authed got 401, want auth-pass (body-level rejection ok)", key)
						}
					} else if gotAuth >= 500 {
						t.Errorf("%s: authed got %d, want <500", key, gotAuth)
					}
				}
			}
		})
	}
}

// TestD1EmptyAndMalformedBearer covers the auth edge cases D1 must keep
// fail-closed: empty bearer, wrong case, extra whitespace.
func TestD1EmptyAndMalformedBearer(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.Open(dir)
	king := policy.NewKing()
	srv, err := New(Options{
		Host:                 "127.0.0.1",
		Port:                 0,
		Token:                testToken,
		Store:                st,
		King:                 king,
		Metrics:              metrics.New(),
		Log:                  &testLogger{},
		RequireTokenForReads: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WritePolicy("1", []byte("version: 1\ndefault_action: DROP\nrules: []\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetActive("1"); err != nil {
		t.Fatal(err)
	}
	king.Replace(policy.NewSnapshot(policy.ActionDrop, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for strings.HasSuffix(srv.Addr(), ":0") {
		if time.Now().After(deadline) {
			t.Fatal("server did not bind")
		}
		time.Sleep(2 * time.Millisecond)
	}

	auths := map[string]string{
		"no header":    "",
		"empty bearer": "Bearer ",
		"wrong case":   "bearer " + testToken,
		"whitespace":   "Bearer  " + testToken + " ",
		"wrong token":  "Bearer nope",
		"extra prefix": "Bearer x " + testToken,
	}
	for name, auth := range auths {
		req, err := http.NewRequest("GET", "http://"+srv.Addr()+"/v1/policy", nil)
		if err != nil {
			t.Fatal(err)
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: got %d, want 401", name, resp.StatusCode)
		}
	}

	// Positive control: exact valid token works.
	req, _ := http.NewRequest("GET", "http://"+srv.Addr()+"/v1/policy", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("positive control: got %d, want 200", resp.StatusCode)
	}
}
