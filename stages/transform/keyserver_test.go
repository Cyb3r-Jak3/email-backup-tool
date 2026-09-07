package transform

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNormalizeFingerprint(t *testing.T) {
	valid := "0123456789ABCDEF0123456789ABCDEF01234567"
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"already clean", valid, valid, false},
		{"lowercase", strings.ToLower(valid), valid, false},
		{"gpg spacing", "0123 4567 89AB CDEF 0123  4567 89AB CDEF 0123 4567", valid, false},
		{"too short", "0123456789ABCDEF", "", true},
		{"not hex", strings.Repeat("Z", 40), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeFingerprint(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKeyserverLookupURL(t *testing.T) {
	got, err := keyserverLookupURL("keys.openpgp.org", "44FE09DEE9E9EC21EF903F06CA614AB6BD73BB06")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("lookup url is not a valid url: %v", err)
	}
	if parsed.Scheme != "https" {
		t.Fatalf("expected https, got %q", parsed.Scheme)
	}
	if parsed.Path != "/pks/lookup" {
		t.Fatalf("unexpected path %q", parsed.Path)
	}
	q := parsed.Query()
	if q.Get("op") != "get" || q.Get("options") != "mr" || q.Get("search") != "0x44FE09DEE9E9EC21EF903F06CA614AB6BD73BB06" {
		t.Fatalf("unexpected query %q", parsed.RawQuery)
	}
}

func TestFetchPublicKey(t *testing.T) {
	fingerprint := "44FE09DEE9E9EC21EF903F06CA614AB6BD73BB06"
	const keyBody = pgpPublicBlock + "\n...\n-----END PGP PUBLIC KEY BLOCK-----\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("search"); got != "0x"+fingerprint {
			t.Errorf("unexpected search query %q", got)
		}
		_, _ = w.Write([]byte(keyBody))
	}))
	defer server.Close()

	got, err := FetchPublicKey(t.Context(), fingerprint, server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != keyBody {
		t.Fatalf("got %q, want %q", got, keyBody)
	}
}

func TestFetchPublicKeyNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := FetchPublicKey(t.Context(), "44FE09DEE9E9EC21EF903F06CA614AB6BD73BB06", server.URL)
	if err == nil {
		t.Fatal("expected an error for a 404 response")
	}
}

func TestFetchPublicKeyInvalidFingerprint(t *testing.T) {
	_, err := FetchPublicKey(t.Context(), "not-a-fingerprint", DefaultKeyServer)
	if err == nil {
		t.Fatal("expected an error for an invalid fingerprint")
	}
}
