package transform

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// DefaultKeyServer is used when a public key fingerprint is configured
// without an explicit server.
const DefaultKeyServer = "keys.openpgp.org"

// keyserverTimeout bounds a single lookup so a slow or unreachable keyserver
// fails a run instead of hanging it.
const keyserverTimeout = 30 * time.Second

// maxKeyserverResponse caps how much of a keyserver response is read, well
// above the size of any real OpenPGP public key.
const maxKeyserverResponse = 1 << 20 // 1 MiB

// fingerprintPattern matches a bare OpenPGP v4 fingerprint: 40 hex digits.
// Short and long key IDs, which keyservers also accept, are rejected because
// they are not collision resistant and so are not a safe way to pin a key.
var fingerprintPattern = regexp.MustCompile(`^[0-9A-Fa-f]{40}$`)

// NormalizeFingerprint strips the spaces gpg prints between groups and
// upper-cases the result, rejecting anything that is not a full 40-digit hex
// fingerprint.
func NormalizeFingerprint(fingerprint string) (string, error) {
	cleaned := strings.ToUpper(strings.ReplaceAll(fingerprint, " ", ""))
	if !fingerprintPattern.MatchString(cleaned) {
		return "", fmt.Errorf("invalid openpgp fingerprint %q: expected 40 hex digits", fingerprint)
	}
	return cleaned, nil
}

// FetchPublicKey retrieves an armoured OpenPGP public key by its full
// fingerprint from a keyserver speaking the HKP lookup protocol, the one
// keys.openpgp.org (the default) and most other keyservers implement.
func FetchPublicKey(ctx context.Context, fingerprint, server string) ([]byte, error) {
	fingerprint, err := NormalizeFingerprint(fingerprint)
	if err != nil {
		return nil, err
	}
	if server == "" {
		server = DefaultKeyServer
	}
	lookupURL, err := keyserverLookupURL(server, fingerprint)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, keyserverTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building keyserver request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching public key %s from %s: %w", fingerprint, server, err)
	}
	defer resp.Body.Close() //nolint:errcheck // reading has already happened or failed
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxKeyserverResponse))
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", server, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("keyserver %s has no key for fingerprint %s (status %s)", server, fingerprint, resp.Status)
	}
	if !strings.Contains(string(body), pgpPublicBlock) {
		return nil, fmt.Errorf("keyserver %s did not return an armoured OpenPGP public key for %s", server, fingerprint)
	}
	return body, nil
}

// keyserverLookupURL builds the HKP `op=get` URL for a fingerprint. server
// may be a bare host, which is reached over HTTPS, or a URL with its own
// scheme.
func keyserverLookupURL(server, fingerprint string) (string, error) {
	if !strings.Contains(server, "://") {
		server = "https://" + server
	}
	base, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("invalid keyserver %q: %w", server, err)
	}
	base.Path = strings.TrimSuffix(base.Path, "/") + "/pks/lookup"
	query := base.Query()
	query.Set("op", "get")
	query.Set("options", "mr")
	query.Set("search", "0x"+fingerprint)
	base.RawQuery = query.Encode()
	return base.String(), nil
}
