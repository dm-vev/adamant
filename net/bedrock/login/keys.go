package login

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"
)

// discoveryURL is the URL of the discovery service of the game, which
// names its other services; the version of the game follows it.
var discoveryURL = "https://client.discovery.minecraft-services.net/api/v1.0/discovery/MinecraftPE/builds/"

// keyRefresh is how long the keys of a service go before a token that
// names a key not held fetches them again.
const keyRefresh = 30 * time.Minute

// Keys verify the multiplayer tokens the authorization service of the game
// signs for players logged in with Xbox Live: they hold the issuer of the
// service, the algorithms it signs with and the keys it publishes.
type Keys struct {
	issuer  string
	jwksURL string
	algs    []string
	client  *http.Client

	mu      sync.Mutex
	keys    []signingKey
	fetched time.Time
	// fetchMu lets one login fetch the keys at a time; the others take
	// what it fetched.
	fetchMu sync.Mutex
}

// signingKey is a key of the service by its ID.
type signingKey struct {
	id  string
	key crypto.PublicKey
}

// Discover finds the authorization service the game of a version logs in
// with and returns the keys of its tokens. The client makes the requests,
// those for the keys later too; nil means http.DefaultClient.
func Discover(ctx context.Context, client *http.Client, version string) (*Keys, error) {
	if client == nil {
		client = http.DefaultClient
	}
	var d struct {
		Result struct {
			ServiceEnvironments map[string]map[string]json.RawMessage `json:"serviceEnvironments"`
		} `json:"result"`
	}
	if err := getJSON(ctx, client, discoveryURL+url.PathEscape(version), &d); err != nil {
		return nil, fmt.Errorf("login: discover services: %w", err)
	}
	raw, ok := d.Result.ServiceEnvironments["auth"]["prod"]
	if !ok {
		return nil, errors.New("login: discovery names no authorization service")
	}
	var env struct {
		Issuer string `json:"issuer"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("login: authorization service: %w", err)
	}
	return NewKeys(ctx, client, env.Issuer)
}

// NewKeys returns the keys of the tokens of an authorization service by its
// issuer, reading where it publishes them and how it signs from its OpenID
// configuration. The client makes the requests; nil means
// http.DefaultClient.
func NewKeys(ctx context.Context, client *http.Client, issuer string) (*Keys, error) {
	if client == nil {
		client = http.DefaultClient
	}
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("login: issuer %q is not a URL", issuer)
	}
	var config struct {
		JWKSURL    string   `json:"jwks_uri"`
		Algorithms []string `json:"id_token_signing_alg_values_supported"`
	}
	if err := getJSON(ctx, client, u.JoinPath("/.well-known/openid-configuration").String(), &config); err != nil {
		return nil, fmt.Errorf("login: OpenID configuration: %w", err)
	}
	if config.JWKSURL == "" {
		return nil, errors.New("login: OpenID configuration names no keys")
	}
	// Tokens name the issuer with a slash at the end of its path.
	k := &Keys{issuer: u.JoinPath().String(), jwksURL: config.JWKSURL, client: client}
	var other []string
	for _, alg := range config.Algorithms {
		switch alg {
		case rs256, es384:
			k.algs = append(k.algs, alg)
		case "":
		default:
			other = append(other, alg)
		}
	}
	if len(k.algs) == 0 {
		if len(other) > 0 {
			return nil, fmt.Errorf("login: the service signs with %q only", other)
		}
		// A configuration that names none signs as OpenID does by default.
		k.algs = []string{rs256}
	}
	return k, nil
}

// getJSON fetches a JSON document of a MiB at most into v.
func getJSON(ctx context.Context, client *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

// verify checks a multiplayer token: signed by a key of the service, of
// its issuer, for multiplayer and not expired at now. It returns the
// members of the payload.
func (k *Keys) verify(ctx context.Context, token string, now time.Time) (map[string]json.RawMessage, error) {
	t, err := parseJWS(token, k.algs...)
	if err != nil {
		return nil, err
	}
	if err := k.verifySignature(ctx, t); err != nil {
		return nil, err
	}
	m, err := object(t.payload)
	if err != nil {
		return nil, fmt.Errorf("login: token payload: %w", err)
	}
	c, err := readClaims(m)
	if err != nil {
		return nil, fmt.Errorf("login: token claims: %w", err)
	}
	switch {
	case c.issuer != k.issuer:
		return nil, fmt.Errorf("login: token issued by %q, not %q", c.issuer, k.issuer)
	case !slices.Contains(c.audience, multiplayerAudience):
		return nil, fmt.Errorf("login: token for %q, not %q", c.audience, multiplayerAudience)
	case c.expiry == nil || c.expiry.time().Before(now):
		// As go-oidc, the expiry of a token of the service has no leeway.
		return nil, errors.New("login: token expired")
	}
	if err := c.valid(now); err != nil {
		return nil, err
	}
	return m, nil
}

// verifySignature checks the signature of a token with the keys held, and
// with those fetched again if the token names a key not held and the keys
// are old enough, or if none are held.
func (k *Keys) verifySignature(ctx context.Context, t *jws) error {
	keys, fetched := k.held()
	if t.verifyAny(keys) {
		return nil
	}
	if len(keys) == 0 || t.kid != "" && !slices.ContainsFunc(keys, func(s signingKey) bool { return s.id == t.kid }) && time.Since(fetched) >= keyRefresh {
		keys, err := k.fetch(ctx, fetched)
		if err != nil {
			return fmt.Errorf("login: keys of the authorization service: %w", err)
		}
		if t.verifyAny(keys) {
			return nil
		}
	}
	return errSignature
}

// verifyAny reports whether a key verifies the token, of the ID the token
// names if it names one.
func (t *jws) verifyAny(keys []signingKey) bool {
	for _, k := range keys {
		if (t.kid == "" || k.id == t.kid) && t.verify(k.key) == nil {
			return true
		}
	}
	return false
}

// held returns the keys held and when they were fetched.
func (k *Keys) held() ([]signingKey, time.Time) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.keys, k.fetched
}

// fetch fetches the keys, unless another login fetched them after a time.
func (k *Keys) fetch(ctx context.Context, after time.Time) ([]signingKey, error) {
	k.fetchMu.Lock()
	defer k.fetchMu.Unlock()
	if keys, fetched := k.held(); fetched.After(after) {
		return keys, nil
	}
	var set struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := getJSON(ctx, k.client, k.jwksURL, &set); err != nil {
		return nil, err
	}
	var keys []signingKey
	for _, raw := range set.Keys {
		if key, ok := parseJWK(raw); ok {
			keys = append(keys, key)
		}
	}
	k.mu.Lock()
	k.keys, k.fetched = keys, time.Now()
	k.mu.Unlock()
	return keys, nil
}

// parseJWK returns the key of a JSON web key that signs: an RSA key of 2048
// bits at least, or a P-384 key. A key of another kind is left out.
func parseJWK(raw json.RawMessage) (signingKey, bool) {
	var j struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Use string `json:"use"`
		Crv string `json:"crv"`
		N   string `json:"n"`
		E   string `json:"e"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}
	if json.Unmarshal(raw, &j) != nil || j.Use != "" && j.Use != "sig" {
		return signingKey{}, false
	}
	switch j.Kty {
	case "RSA":
		n, errN := b64decode(j.N)
		e, errE := b64decode(j.E)
		if errN != nil || errE != nil || len(e) == 0 || len(e) > 4 {
			return signingKey{}, false
		}
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
		if pub.N.BitLen() < 2048 || pub.E < 3 || pub.E%2 == 0 {
			return signingKey{}, false
		}
		return signingKey{j.Kid, pub}, true
	case "EC":
		x, errX := b64decode(j.X)
		y, errY := b64decode(j.Y)
		if j.Crv != "P-384" || errX != nil || errY != nil || len(x) != 48 || len(y) != 48 {
			return signingKey{}, false
		}
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P384(), append(append([]byte{4}, x...), y...))
		if err != nil {
			return signingKey{}, false
		}
		return signingKey{j.Kid, pub}, true
	}
	return signingKey{}, false
}
