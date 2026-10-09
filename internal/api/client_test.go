package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/NodeOps-app/createos-cli/internal/httpclient"
)

func TestClientsSendUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.UserAgent(); got != httpclient.UserAgent {
			t.Errorf("User-Agent = %q, want %q", got, httpclient.UserAgent)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	clients := map[string]*resty.Client{
		"API key":         NewClient("key", server.URL, false).Client,
		"OAuth":           NewClientWithAccessToken("token", server.URL, false, nil).Client,
		"sandbox API key": NewSandboxClient("key", server.URL, false).Client,
		"sandbox OAuth":   NewSandboxClientWithAccessToken("token", server.URL, false, nil).Client,
	}
	for name, client := range clients {
		t.Run(name, func(t *testing.T) {
			if _, err := client.R().Get("/"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
