package login

import (
	"context"
	"crypto/ecdsa"
	"crypto/md5"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Login is the login request of a client as a server read it, its
// signatures checked.
type Login struct {
	// Identity names the player. The authorization service vouches for it
	// if XboxLive is set; else the client named itself.
	Identity Identity
	// ClientData describes the device and the skin of the client, as the
	// client says. ClientDataJSON is the JSON it was read from, for a
	// server that reads it into types of its own.
	ClientData     ClientData
	ClientDataJSON []byte
	// Key is the public key of the client, which signed the client data
	// and which the client proves it holds when it connects.
	Key *ecdsa.PublicKey
	// XboxLive reports a player logged in with Xbox Live: one with an XUID
	// in a token the authorization service signed.
	XboxLive bool
}

// guest is the authentication type of a guest, which servers refuse.
const guest = 1

// Parse reads the connection request of a Login packet and checks its
// signatures: the multiplayer token with keys, or, keys being nil, as a
// token the client may have signed itself; the client data with the key
// the token names. The times of the claims signed by others than the
// client are checked against now.
func Parse(ctx context.Context, request []byte, keys *Keys, now time.Time) (Login, error) {
	req, err := parseRequest(request)
	if err != nil {
		return Login{}, err
	}
	var l Login
	switch {
	case req.token != "" && keys != nil:
		m, err := keys.verify(ctx, req.token, now)
		if err != nil {
			return Login{}, fmt.Errorf("login: verify multiplayer token: %w", err)
		}
		if l.Identity, l.Key, err = tokenIdentity(m); err != nil {
			return Login{}, err
		}
		l.XboxLive = l.Identity.XUID != ""
		if l.XboxLive && len(req.chain) > 0 {
			// The token does not name the title of the game the player
			// plays, which the chain clients still send beside it does.
			if id, _, xbox, err := parseChain(req.chain, now); err == nil && xbox && id.TitleID != "" && id.XUID == l.Identity.XUID {
				l.Identity.TitleID = id.TitleID
			}
		}
	case req.token != "":
		// Nothing vouches for a token without keys: whoever signed it, the
		// client names itself.
		t, err := parseJWS(req.token, es384, rs256)
		if err != nil {
			return Login{}, fmt.Errorf("login: multiplayer token: %w", err)
		}
		m, err := object(t.payload)
		if err != nil {
			return Login{}, fmt.Errorf("login: multiplayer token: %w", err)
		}
		if l.Identity, l.Key, err = tokenIdentity(m); err != nil {
			return Login{}, err
		}
	default:
		if l.Identity, l.Key, l.XboxLive, err = parseChain(req.chain, now); err != nil {
			return Login{}, err
		}
	}
	t, err := parseJWS(req.clientData, es384)
	if err != nil {
		return Login{}, fmt.Errorf("login: client data: %w", err)
	}
	if err := t.verify(l.Key); err != nil {
		return Login{}, fmt.Errorf("login: client data: %w", err)
	}
	if _, err := object(t.payload); err != nil {
		return Login{}, fmt.Errorf("login: client data: %w", err)
	}
	if err := json.Unmarshal(t.payload, &l.ClientData); err != nil {
		return Login{}, fmt.Errorf("login: client data: %w", err)
	}
	l.ClientDataJSON = t.payload
	l.ClientData.ServerAddress = bracketIPv6(l.ClientData.ServerAddress)
	return l, nil
}

// connectionRequest is a connection request split into its parts.
type connectionRequest struct {
	chain      []string
	token      string
	clientData string
}

// parseRequest splits a connection request: a JSON object with the token
// and the certificate chain, then the JWS of the client data, each after
// its length as a little-endian int32.
func parseRequest(b []byte) (connectionRequest, error) {
	head, b, err := part(b)
	if err != nil {
		return connectionRequest{}, fmt.Errorf("login: certificate: %w", err)
	}
	var j struct {
		Certificate        string
		AuthenticationType uint8
		Token              string
		// Chain is where requests of older versions held the chain.
		Chain []string `json:"chain"`
	}
	if err := json.Unmarshal(head, &j); err != nil {
		return connectionRequest{}, fmt.Errorf("login: certificate: %w", err)
	}
	req := connectionRequest{chain: j.Chain, token: j.Token}
	if j.Certificate != "" {
		var cert struct {
			Chain []string `json:"chain"`
		}
		if err := json.Unmarshal([]byte(j.Certificate), &cert); err != nil {
			return connectionRequest{}, fmt.Errorf("login: certificate: %w", err)
		}
		req.chain = cert.Chain
	}
	if j.AuthenticationType == guest {
		return connectionRequest{}, errors.New("login: guests may not log in")
	}
	data, b, err := part(b)
	if err != nil {
		return connectionRequest{}, fmt.Errorf("login: client data: %w", err)
	}
	if len(b) != 0 {
		return connectionRequest{}, fmt.Errorf("login: %d bytes after the client data", len(b))
	}
	req.clientData = string(data)
	return req, nil
}

// part reads a part of a connection request after its length.
func part(b []byte) (p, rest []byte, err error) {
	if len(b) < 4 {
		return nil, nil, errors.New("connection request ends before a length")
	}
	n := int32(binary.LittleEndian.Uint32(b))
	if n < 0 || int(n) > len(b)-4 {
		return nil, nil, fmt.Errorf("length %d of %d bytes", n, len(b)-4)
	}
	return b[4 : 4+n], b[4+n:], nil
}

// tokenIdentity reads the player and the key of the client from the
// claims of a multiplayer token.
func tokenIdentity(m map[string]json.RawMessage) (Identity, *ecdsa.PublicKey, error) {
	var id Identity
	var cpk string
	if err := members(m, "cpk", &cpk, "xid", &id.XUID, "xname", &id.DisplayName,
		"mid", &id.PlayFabID, "tid", &id.PlayFabTitleID, "leguuid", &id.Identity); err != nil {
		return Identity{}, nil, fmt.Errorf("login: multiplayer token: %w", err)
	}
	key, err := parsePublicKey(cpk)
	if err != nil {
		return Identity{}, nil, fmt.Errorf("login: multiplayer token: %w", err)
	}
	if id.Identity == "" {
		// An offline client names its UUID; the UUID of a player of Xbox
		// Live follows from its XUID.
		id.Identity = NewUUID()
		if id.XUID != "" {
			id.Identity = xuidUUID(id.XUID)
		}
	}
	return id, key, nil
}

// xuidUUID returns the UUID the game derives from an XUID: version 3, of
// the MD5 of the XUID in the name space the game names.
func xuidUUID(xuid string) string {
	u := md5.Sum([]byte("pocket-auth-1-xuid:" + xuid))
	u[6] = u[6]&0x0f | 0x30
	u[8] = u[8]&0x3f | 0x80
	return formatUUID(u)
}

// formatUUID formats a UUID in the 8-4-4-4-12 form of lowercase hex.
func formatUUID(u [16]byte) string {
	h := hex.EncodeToString(u[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// mojangKey is the key of Mojang, which signs the second token of the
// chain of a player logged in with Xbox Live.
var mojangKey = func() *ecdsa.PublicKey {
	key, err := parsePublicKey("MHYwEAYHKoZIzj0CAQYFK4EEACIDYgAECRXueJeTDqNRRgJi/vlRufByu/2G0i2Ebt6YMar5QX/R0DIIyrJMcUpruK4QveTfJSTp3Shlq4Gk34cD/4GUWwkv0DVuzeuB+tXija7HBxii03NHDbPAD0AKnLr2wdAp")
	if err != nil {
		panic(err)
	}
	return key
}()

// parseChain checks the certificate chain of a login and returns the
// player it names, the key of the client and whether Mojang signed it for
// a player of Xbox Live. A chain of one token is signed by the client
// itself; one of three goes from the key of the client to the key of
// Mojang, which signs the key Xbox Live signs the player with.
func parseChain(chain []string, now time.Time) (Identity, *ecdsa.PublicKey, bool, error) {
	if len(chain) != 1 && len(chain) != 3 {
		return Identity{}, nil, false, fmt.Errorf("login: chain of %d tokens", len(chain))
	}
	first, err := parseJWS(chain[0], es384)
	if err != nil {
		return Identity{}, nil, false, fmt.Errorf("login: chain: %w", err)
	}
	// The first token names the key it is signed with.
	key, err := parsePublicKey(first.x5u)
	if err != nil {
		return Identity{}, nil, false, fmt.Errorf("login: chain: %w", err)
	}
	tokens := []*jws{first}
	for _, s := range chain[1:] {
		t, err := parseJWS(s, es384)
		if err != nil {
			return Identity{}, nil, false, fmt.Errorf("login: chain: %w", err)
		}
		tokens = append(tokens, t)
	}
	xbox := false
	var last map[string]json.RawMessage
	for i, t := range tokens {
		if last, key, err = chainToken(t, key, now, i > 0); err != nil {
			return Identity{}, nil, false, fmt.Errorf("login: chain token %d: %w", i, err)
		}
		if i == 0 && len(tokens) == 3 {
			// The first token of a chain of Xbox Live hands over to the key
			// of Mojang.
			xbox = key.Equal(mojangKey)
		}
	}
	var data struct {
		XUID        string `json:"XUID"`
		Identity    string `json:"identity"`
		DisplayName string `json:"displayName"`
		TitleID     string `json:"titleId"`
	}
	extra, ok := last["extraData"]
	if !ok {
		return Identity{}, nil, false, errors.New("login: chain names no player")
	}
	m, err := object(extra)
	if err != nil {
		return Identity{}, nil, false, fmt.Errorf("login: chain: %w", err)
	}
	if err := members(m, "XUID", &data.XUID, "identity", &data.Identity, "displayName", &data.DisplayName, "titleId", &data.TitleID); err != nil {
		return Identity{}, nil, false, fmt.Errorf("login: chain: %w", err)
	}
	if len(tokens) == 3 && xbox != (data.XUID != "") {
		return Identity{}, nil, false, errors.New("login: chain of Mojang names an XUID unless Xbox Live signed it")
	}
	return Identity{XUID: data.XUID, Identity: data.Identity, DisplayName: data.DisplayName, TitleID: data.TitleID}, key, xbox, nil
}

// chainToken checks a token of a chain with the key the token before
// handed over, and returns its claims and the key it hands over in turn:
// the identityPublicKey it names, else the key it was signed with. A token
// after the first must be issued by Mojang.
func chainToken(t *jws, key *ecdsa.PublicKey, now time.Time, mojang bool) (map[string]json.RawMessage, *ecdsa.PublicKey, error) {
	if err := t.verify(key); err != nil {
		return nil, nil, err
	}
	m, err := object(t.payload)
	if err != nil {
		return nil, nil, err
	}
	c, err := readClaims(m)
	if err != nil {
		return nil, nil, err
	}
	if err := c.valid(now); err != nil {
		return nil, nil, err
	}
	if mojang && c.issuer != "Mojang" {
		return nil, nil, fmt.Errorf("issued by %q, not Mojang", c.issuer)
	}
	if raw, ok := m["identityPublicKey"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, nil, fmt.Errorf("identityPublicKey: %w", err)
		}
		if key, err = parsePublicKey(s); err != nil {
			return nil, nil, err
		}
	}
	return m, key, nil
}

// parsePublicKey reads a public key as the login encodes it: base64 of its
// DER SubjectPublicKeyInfo, an ECDSA key.
func parsePublicKey(s string) (*ecdsa.PublicKey, error) {
	der, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}
	pub, ok := key.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is a %T, not an ECDSA key", key)
	}
	return pub, nil
}

// bracketIPv6 puts brackets around the IPv6 address of a server address
// the game may write without them, so that its port can be told apart.
func bracketIPv6(addr string) string {
	if strings.Contains(addr, "://") || strings.Count(addr, ":") < 2 || addr[0] == '[' {
		return addr
	}
	i := strings.LastIndex(addr, ":")
	return "[" + addr[:i] + "]" + addr[i:]
}
