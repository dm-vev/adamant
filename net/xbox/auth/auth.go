// Package auth signs a host in to Xbox Live and the Minecraft services as
// the game does on Android: a Microsoft account through the device code
// flow, an Xbox device token, the SISU authorization, XSTS tokens, a
// PlayFab session and the MCToken of the Minecraft services, which carries
// the player messaging ID the signaling service knows the host by. A
// Session keeps the tokens fresh and hands out a Snapshot that signs the
// account in again without the user.
package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Config is the title and device a host signs in as.
type Config struct {
	ClientID      string
	TitleID       int64
	UserAgent     string
	DeviceType    string
	DeviceVersion string
	Sandbox       string
	// PlayFabTitle is the PlayFab title of the game.
	PlayFabTitle string
	// GameVersion is the version of the game the Minecraft services are
	// told of.
	GameVersion string
	// TreatmentOverrides are asked of the Minecraft services with the
	// MCToken.
	TreatmentOverrides []string
	// Endpoints are the services. Zero values mean DefaultEndpoints.
	Endpoints Endpoints
	// HTTPClient sends the requests. Nil means http.DefaultClient.
	HTTPClient *http.Client
}

// Android is Minecraft for Android, as gophertunnel and MCXboxBroadcast
// sign in.
var Android = Config{
	ClientID:      "0000000048183522",
	TitleID:       1739947436,
	UserAgent:     "XAL Android 2025.04.20250326.000",
	DeviceType:    "Android",
	DeviceVersion: "13",
	Sandbox:       "RETAIL",
	PlayFabTitle:  "20CA2",
	GameVersion:   "1.26.52",
}

// Endpoints are the base URLs of the services.
type Endpoints struct {
	Live, Device, SISU, XSTS, PlayFab, Minecraft string
}

// DefaultEndpoints are the services of the game.
var DefaultEndpoints = Endpoints{
	Live:      "https://login.live.com",
	Device:    "https://device.auth.xboxlive.com",
	SISU:      "https://sisu.xboxlive.com",
	XSTS:      "https://xsts.auth.xboxlive.com",
	PlayFab:   "https://20ca2.playfabapi.com",
	Minecraft: "https://authorization.franchise.minecraft-services.net",
}

// Relying parties of the XSTS tokens the host needs.
const (
	// XboxLive is the relying party of the Xbox services: the session
	// directory and real-time activity.
	XboxLive = "http://xboxlive.com"
	// PlayFab is the relying party of PlayFab.
	PlayFab = "http://playfab.xboxlive.com/"
)

const (
	scope = "service::user.auth.xboxlive.com::MBI_SSL"
	// refreshMargin is how long before it expires a token is renewed.
	refreshMargin = 5 * time.Minute
	// maxResponse bounds the body of a response read.
	maxResponse = 1 << 20
)

func (c Config) endpoints() Endpoints {
	e := c.Endpoints
	d := DefaultEndpoints
	for _, f := range []struct{ v, def *string }{
		{&e.Live, &d.Live}, {&e.Device, &d.Device}, {&e.SISU, &d.SISU},
		{&e.XSTS, &d.XSTS}, {&e.PlayFab, &d.PlayFab}, {&e.Minecraft, &d.Minecraft},
	} {
		if *f.v == "" {
			*f.v = *f.def
		}
	}
	return e
}

func (c Config) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// Snapshot is what a Session needs to sign its account in again: the
// refresh token of the Microsoft account and the key and ID of the device.
// It holds secrets: whoever has it acts as the account.
type Snapshot struct {
	RefreshToken string `json:"refreshToken"`
	ProofKey     string `json:"proofKey"`
	DeviceID     string `json:"deviceId"`
}

// XSTSToken is an XSTS token for one relying party.
type XSTSToken struct {
	Token    string
	UserHash string
	XUID     string
	Gamertag string
	NotAfter time.Time
}

// Header returns the token as the Authorization header of the services of
// its relying party.
func (t *XSTSToken) Header() string { return "XBL3.0 x=" + t.UserHash + ";" + t.Token }

// MCToken is the token of the Minecraft services.
type MCToken struct {
	// Header is the Authorization header: "MCToken ...".
	Header     string
	ValidUntil time.Time
	// PlayerMessagingID is the ID the signaling service knows the account
	// by.
	PlayerMessagingID string
}

// Session holds the tokens of an account and renews them as they expire.
// It is safe for concurrent use.
type Session struct {
	conf      Config
	ep        Endpoints
	mu        sync.Mutex
	refresh   string
	key       *ecdsa.PrivateKey
	deviceID  string
	access    token
	device    token
	user      token
	title     token
	xsts      map[string]*XSTSToken
	playfab   token
	mc        *MCToken
	skew      time.Duration
	onRefresh func(Snapshot)
}

// token is a token and when it expires.
type token struct {
	value    string
	notAfter time.Time
}

func (t token) fresh(now time.Time) bool {
	return t.value != "" && now.Add(refreshMargin).Before(t.notAfter)
}

// Restore returns a Session signing the account of a Snapshot in again.
func (c Config) Restore(s Snapshot) (*Session, error) {
	der, err := base64.StdEncoding.DecodeString(s.ProofKey)
	if err != nil {
		return nil, fmt.Errorf("auth: proof key: %w", err)
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("auth: proof key: %w", err)
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("auth: proof key is not a P-256 key")
	}
	if s.RefreshToken == "" || s.DeviceID == "" {
		return nil, errors.New("auth: snapshot without refresh token or device ID")
	}
	return &Session{conf: c, ep: c.endpoints(), refresh: s.RefreshToken, key: key, deviceID: s.DeviceID, xsts: map[string]*XSTSToken{}}, nil
}

// newSession returns a Session for a fresh login with a new device.
func (c Config) newSession(access token, refresh string) (*Session, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Session{conf: c, ep: c.endpoints(), refresh: refresh, access: access, key: key, deviceID: "{" + newUUID() + "}", xsts: map[string]*XSTSToken{}}, nil
}

// Snapshot returns what signs the account in again.
func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot()
}

func (s *Session) snapshot() Snapshot {
	der, _ := x509.MarshalPKCS8PrivateKey(s.key)
	return Snapshot{RefreshToken: s.refresh, ProofKey: base64.StdEncoding.EncodeToString(der), DeviceID: s.deviceID}
}

// OnRefresh sets a function called with a new Snapshot whenever the
// refresh token of the account is replaced, which the old one no longer
// signs in after.
func (s *Session) OnRefresh(f func(Snapshot)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onRefresh = f
}

// now returns the time of the services, as their Date headers tell it.
func (s *Session) now() time.Time { return time.Now().Add(s.skew) }

// XSTS returns an XSTS token for a relying party.
func (s *Session) XSTS(ctx context.Context, relyingParty string) (*XSTSToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.xstsLocked(ctx, relyingParty)
}

// MCToken returns the token of the Minecraft services.
func (s *Session) MCToken(ctx context.Context) (*MCToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mc != nil && s.now().Add(refreshMargin).Before(s.mc.ValidUntil) {
		return s.mc, nil
	}
	ticket, err := s.playfabLocked(ctx)
	if err != nil {
		return nil, err
	}
	mc, err := s.startSession(ctx, ticket)
	if err != nil {
		return nil, err
	}
	s.mc = mc
	return mc, nil
}

// post sends a JSON request, signed with the proof key if sign is set, and
// decodes the response into out.
func (s *Session) post(ctx context.Context, rawURL string, body any, header http.Header, sign bool, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	for retried := false; ; retried = true {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(b))
		if err != nil {
			return err
		}
		for k, v := range header {
			req.Header[k] = v
		}
		req.Header.Set("Content-Type", "application/json")
		if req.Header.Get("User-Agent") == "" {
			req.Header.Set("User-Agent", s.conf.UserAgent)
		}
		skew := s.skew
		if sign {
			req.Header.Set("Signature", signature(req, b, s.key, s.now()))
		}
		err = s.do(req, out)
		var se *StatusError
		if sign && !retried && errors.As(err, &se) && (se.Status == http.StatusUnauthorized || se.Status == http.StatusForbidden) &&
			(s.skew-skew > 30*time.Second || skew-s.skew > 30*time.Second) {
			// Signed at a time the service took as wrong: again at its time.
			continue
		}
		return err
	}
}

// do sends a request and decodes the JSON response into out, taking the
// time of the service from its Date header.
func (s *Session) do(req *http.Request, out any) error {
	resp, err := s.conf.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if d, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
		s.skew = time.Until(d)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return &StatusError{URL: req.URL.Host + req.URL.Path, Status: resp.StatusCode, XErr: resp.Header.Get("X-Err"), Body: excerpt(body)}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("auth: %s: %w", req.URL.Host, err)
	}
	return nil
}

// StatusError is a response of a service with a status other than OK.
type StatusError struct {
	URL    string
	Status int
	// XErr is the X-Err header of an Xbox service, which tells why it
	// refused the account: 2148916233 for one without an Xbox profile.
	XErr string
	Body string
}

func (e *StatusError) Error() string {
	s := fmt.Sprintf("auth: %s: status %d", e.URL, e.Status)
	if e.XErr != "" {
		s += " (X-Err " + e.XErr + ")"
	}
	if e.Body != "" {
		s += ": " + e.Body
	}
	return s
}

// excerpt returns the start of a body for an error, printable.
func excerpt(b []byte) string {
	if len(b) > 256 {
		b = b[:256]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, string(b))
}

// signature returns the Signature header of a request to an Xbox service:
// the policy version, a Windows timestamp and an ECDSA signature over them,
// the method, the path and query, the Authorization header and the body.
func signature(req *http.Request, body []byte, key *ecdsa.PrivateKey, now time.Time) string {
	const version = 1
	ts := uint64(now.UnixNano()/100 + 116444736000000000)
	h := sha256.New()
	var head [14]byte
	binary.BigEndian.PutUint32(head[0:], version)
	binary.BigEndian.PutUint64(head[5:], ts)
	h.Write(head[:])
	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}
	for _, part := range []string{req.Method, path, req.Header.Get("Authorization")} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	h.Write(body)
	h.Write([]byte{0})
	r, s, err := ecdsa.Sign(rand.Reader, key, h.Sum(nil))
	if err != nil {
		return ""
	}
	out := make([]byte, 12+64)
	binary.BigEndian.PutUint32(out[0:], version)
	binary.BigEndian.PutUint64(out[4:], ts)
	r.FillBytes(out[12:44])
	s.FillBytes(out[44:76])
	return base64.StdEncoding.EncodeToString(out)
}

// VerifySignature checks the Signature header of a request to an Xbox
// service against the public key of its proof key and returns when it was
// signed, for services and tests that check signatures.
func VerifySignature(req *http.Request, body []byte, pub *ecdsa.PublicKey) (time.Time, error) {
	raw, err := base64.StdEncoding.DecodeString(req.Header.Get("Signature"))
	if err != nil || len(raw) != 76 || binary.BigEndian.Uint32(raw) != 1 {
		return time.Time{}, errors.New("auth: malformed signature")
	}
	ts := binary.BigEndian.Uint64(raw[4:12])
	h := sha256.New()
	var head [14]byte
	binary.BigEndian.PutUint32(head[0:], 1)
	binary.BigEndian.PutUint64(head[5:], ts)
	h.Write(head[:])
	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}
	for _, part := range []string{req.Method, path, req.Header.Get("Authorization")} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	h.Write(body)
	h.Write([]byte{0})
	if !ecdsa.Verify(pub, h.Sum(nil), new(big.Int).SetBytes(raw[12:44]), new(big.Int).SetBytes(raw[44:76])) {
		return time.Time{}, errors.New("auth: signature does not verify")
	}
	return time.Unix(0, int64(ts-116444736000000000)*100), nil
}

// proofKey is the public key of a proof key as a JWK.
type proofKey struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	Alg string `json:"alg"`
	Use string `json:"use"`
}

func jwk(key *ecdsa.PrivateKey) proofKey {
	raw, _ := key.PublicKey.ECDH()
	b := raw.Bytes() // 0x04 || X || Y
	return proofKey{
		Kty: "EC", Crv: "P-256", Alg: "ES256", Use: "sig",
		X: base64.RawURLEncoding.EncodeToString(b[1:33]),
		Y: base64.RawURLEncoding.EncodeToString(b[33:65]),
	}
}

// newUUID returns a random UUID.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// form posts a form to the Microsoft account service.
func (c Config) form(ctx context.Context, path string, values url.Values, out any) (status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoints().Live+path, strings.NewReader(values.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return resp.StatusCode, err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return resp.StatusCode, fmt.Errorf("auth: %s: status %d: %s", path, resp.StatusCode, excerpt(body))
	}
	return resp.StatusCode, nil
}

// expiry returns when a token that lasts seconds expires.
func expiry(now time.Time, seconds int64) time.Time {
	return now.Add(time.Duration(seconds) * time.Second)
}
