// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package oauthkey

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveAuthKey(t *testing.T) {
	tests := []struct {
		name        string
		clientID    string
		tags        []string
		wantAuthKey string
		wantErr     string
	}{
		{
			name:        "something not an oauth client secret passes through unchanged",
			clientID:    "tskey-auth-regular",
			tags:        []string{"tag:test"},
			wantAuthKey: "tskey-auth-regular",
			wantErr:     "",
		},
		{
			name:        "oauth client secret without tags",
			clientID:    "tskey-client-abc",
			tags:        nil,
			wantAuthKey: "",
			wantErr:     "oauth authkeys require --advertise-tags",
		},
		{
			name:        "oauth client secret with default attributes",
			clientID:    "tskey-client-abc",
			tags:        []string{"tag:test"},
			wantAuthKey: "tskey-auth-xyz",
			wantErr:     "",
		},
		{
			name:        "oauth client secret with custom attributes",
			clientID:    "tskey-client-abc?ephemeral=false&preauthorized=true",
			tags:        []string{"tag:test"},
			wantAuthKey: "tskey-auth-xyz",
			wantErr:     "",
		},
		{
			name:        "oauth client secret with unknown attribute",
			clientID:    "tskey-client-abc?unknown=value",
			tags:        []string{"tag:test"},
			wantAuthKey: "",
			wantErr:     `unknown attribute "unknown"`,
		},
		{
			name:        "oauth client secret with invalid boolean",
			clientID:    "tskey-client-abc?ephemeral=invalid",
			tags:        []string{"tag:test"},
			wantAuthKey: "",
			wantErr:     `invalid attribute boolean attribute ephemeral value "invalid"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := mockControlServer(t)
			defer srv.Close()

			// resolveAuthKey reads custom control plane URLs off the baseURL attribute
			// on the client secret string. Therefore, append the baseURL attribute with
			// the mock control server URL to any client secret.
			if strings.HasPrefix(tt.clientID, "tskey-client") {
				if !strings.Contains(tt.clientID, "?") {
					tt.clientID += "?baseURL=" + srv.URL
				} else {
					tt.clientID += "&baseURL=" + srv.URL
				}
			}

			got, err := resolveAuthKey(context.Background(), tt.clientID, tt.tags)

			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("want no error, got: %q", tt.wantErr)
					return
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("want error to contain %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("want no error, got %q", err)
				return
			}

			if got != tt.wantAuthKey {
				t.Errorf("want authKey = %q, got %q", tt.wantAuthKey, got)
			}
		})
	}
}

func TestResolveAuthKeyAttributes(t *testing.T) {
	tests := []struct {
		name          string
		clientSecret  string
		wantEphemeral bool
		wantPreauth   bool
		wantBaseURL   string
	}{
		{
			name:          "default values",
			clientSecret:  "tskey-client-abc",
			wantEphemeral: true,
			wantPreauth:   false,
			wantBaseURL:   "https://api.tailscale.com",
		},
		{
			name:          "ephemeral=false",
			clientSecret:  "tskey-client-abc?ephemeral=false",
			wantEphemeral: false,
			wantPreauth:   false,
			wantBaseURL:   "https://api.tailscale.com",
		},
		{
			name:          "preauthorized=true",
			clientSecret:  "tskey-client-abc?preauthorized=true",
			wantEphemeral: true,
			wantPreauth:   true,
			wantBaseURL:   "https://api.tailscale.com",
		},
		{
			name:          "baseURL=https://api.example.com",
			clientSecret:  "tskey-client-abc?baseURL=https://api.example.com",
			wantEphemeral: true,
			wantPreauth:   false,
			wantBaseURL:   "https://api.example.com",
		},
		{
			name:          "all custom values",
			clientSecret:  "tskey-client-abc?ephemeral=false&preauthorized=true&baseURL=https://api.example.com",
			wantEphemeral: false,
			wantPreauth:   true,
			wantBaseURL:   "https://api.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strippedSecret, ephemeral, preauth, baseURL, err := parseOptionalAttributes(tt.clientSecret)
			if err != nil {
				t.Fatalf("want no error, got %q", err)
			}
			if strippedSecret != "tskey-client-abc" {
				t.Errorf("want tskey-client-abc, got %q", strippedSecret)
			}
			if ephemeral != tt.wantEphemeral {
				t.Errorf("want ephemeral = %v, got %v", tt.wantEphemeral, ephemeral)
			}
			if preauth != tt.wantPreauth {
				t.Errorf("want preauth = %v, got %v", tt.wantPreauth, preauth)
			}
			if baseURL != tt.wantBaseURL {
				t.Errorf("want baseURL = %v, got %v", tt.wantBaseURL, baseURL)
			}
		})
	}
}

func mockControlServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/v2/oauth/token"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"access_token":"access-123","token_type":"Bearer","expires_in":3600}`))
		case strings.Contains(r.URL.Path, "/api/v2/tailnet") && strings.Contains(r.URL.Path, "/keys"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"key":"tskey-auth-xyz"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}
