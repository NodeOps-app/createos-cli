package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListAPIKeyPreviewPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"maskedSecret":"abcdef123*****987xy"},{"maskedSecret":"skp-abcde*****987xy"},{}]}`)
	}))
	defer server.Close()

	client := NewClient("test", server.URL, false)
	keys, err := client.ListAPIKeys(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"skp-abcdef123*****987xy", "skp-abcde*****987xy", ""}
	if len(keys) != len(want) {
		t.Fatalf("got %d keys, want %d", len(keys), len(want))
	}
	for i, preview := range want {
		if keys[i].MaskedSecret != preview {
			t.Errorf("preview %d = %q, want %q", i, keys[i].MaskedSecret, preview)
		}
	}
}
