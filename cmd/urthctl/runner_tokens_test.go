package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sre-norns/wyrd/identity/cli"
)

func TestRunnerTokenLifecycleUsesSharedIdentityAndRedactedReads(t *testing.T) {
	const secret = "one-time-token-secret"
	const token = `{"apiVersion":"identity.sre-norns.com/v1","kind":"agent-identity-tokens","metadata":{"uid":"token-id","name":"installation","version":2},"spec":{"agentId":"runner-id"},"status":{"phase":"active"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer owner" {
			t.Error("token lifecycle must use the signed-in authority")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/accounts/account/runners/runner":
			fmt.Fprint(w, `{"apiVersion":"urth.sre-norns.com/v1","kind":"runners","metadata":{"uid":"runner-id","name":"runner","version":1},"spec":{"active":true},"status":{}}`)
		case r.Method == "POST" && r.URL.Path == "/v1/agent-identities/runner-id/tokens":
			if r.Header.Get("Idempotency-Key") == "" {
				t.Error("issuance needs an idempotency key")
			}
			fmt.Fprintf(w, `{"resource":%s,"token":%q}`, token, secret)
		case r.Method == "GET" && r.URL.Path == "/v1/agent-identities/runner-id/tokens":
			fmt.Fprintf(w, `{"items":[%s]}`, token)
		case r.Method == "GET" && r.URL.Path == "/v1/agent-identity-tokens/token-id":
			fmt.Fprint(w, token)
		case r.Method == "PATCH" && r.URL.Path == "/v1/agent-identity-tokens/token-id":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != `{"operation":"revoke"}` || r.Header.Get("If-Match") != `"2"` {
				t.Errorf("revocation lacks the read version or canonical operation: %s %s", r.Header.Get("If-Match"), body)
			}
			fmt.Fprintf(w, `{"resource":%s}`, strings.ReplaceAll(token, `"active"`, `"revoked"`))
		default:
			t.Errorf("unexpected operation: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	for _, args := range [][]string{
		{"runners", "tokens", "issue", "runner", "installation"},
		{"runners", "tokens", "list", "runner"},
		{"runners", "tokens", "get", "token-id"},
		{"runners", "tokens", "revoke", "token-id"},
	} {
		parsed, appCli, cfg := parse(t, append(args, "--token=owner", "--account=account", "--api-server-address="+server.URL)...)
		var out bytes.Buffer
		cfg.Env.Output = cli.Output{Format: cli.FormatJSON, Stdout: &out}
		if err := prepareCommand(parsed, appCli, cfg); err != nil {
			t.Fatal(err)
		}
		if err := parsed.Run(cfg); err != nil {
			t.Fatal(err)
		}
		if args[2] == "issue" {
			if !strings.Contains(out.String(), secret) || !strings.Contains(out.String(), `"resource"`) {
				t.Fatal("explicit issue output must disclose the secret in its operation envelope")
			}
		} else if strings.Contains(out.String(), secret) || strings.Contains(out.String(), `"token"`) {
			t.Fatal("ordinary output must exclude the issued secret")
		}
	}
}

func TestRunnerTokenIssueExpiry(t *testing.T) {
	for _, tc := range []struct {
		name, expiry string
		invalid      bool
	}{
		{name: "default"},
		{name: "future", expiry: "2099-01-02T03:04:05Z"},
		{name: "invalid", expiry: "tomorrow", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posted := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "GET" {
					fmt.Fprint(w, `{"apiVersion":"urth.sre-norns.com/v1","kind":"runners","metadata":{"uid":"runner-id","name":"runner","version":1},"spec":{"active":true},"status":{}}`)
					return
				}
				posted = true
				var body struct {
					Spec struct {
						ExpiresAt *string `json:"expiresAt"`
					} `json:"spec"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if tc.expiry == "" && body.Spec.ExpiresAt != nil {
					t.Error("default issuance must omit expiry")
				}
				if tc.expiry != "" && (body.Spec.ExpiresAt == nil || *body.Spec.ExpiresAt != tc.expiry) {
					t.Error("canonical issuance spec must carry requested expiry")
				}
				fmt.Fprint(w, `{"resource":{"apiVersion":"identity.sre-norns.com/v1","kind":"agent-identity-tokens","metadata":{"uid":"token-id","name":"installation","version":1},"spec":{"agentId":"runner-id"},"status":{"phase":"active"}},"token":"one-time-token-secret"}`)
			}))
			defer server.Close()
			args := []string{"runners", "tokens", "issue", "runner", "installation", "--token=owner", "--account=account", "--api-server-address=" + server.URL}
			if tc.expiry != "" {
				args = append(args, "--expires-at="+tc.expiry)
			}
			parsed, appCli, cfg := parse(t, args...)
			var out bytes.Buffer
			cfg.Env.Output = cli.Output{Format: cli.FormatJSON, Stdout: &out}
			if err := prepareCommand(parsed, appCli, cfg); err != nil {
				t.Fatal(err)
			}
			err := parsed.Run(cfg)
			if tc.invalid {
				if err == nil || posted {
					t.Fatal("invalid expiry must fail before issuance")
				}
			} else if err != nil || !posted {
				t.Fatalf("issuance failed: %v", err)
			}
		})
	}
}
