package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// playfabLocked returns the session ticket of the PlayFab account of the
// user, signing in to PlayFab with Xbox Live.
func (s *Session) playfabLocked(ctx context.Context) (string, error) {
	if s.playfab.fresh(s.now()) {
		return s.playfab.value, nil
	}
	x, err := s.xstsLocked(ctx, PlayFab)
	if err != nil {
		return "", err
	}
	var resp struct {
		Data struct {
			SessionTicket string `json:"SessionTicket"`
			EntityToken   struct {
				TokenExpiration time.Time `json:"TokenExpiration"`
			} `json:"EntityToken"`
		} `json:"data"`
	}
	if err := s.post(ctx, s.ep.PlayFab+"/Client/LoginWithXbox", map[string]any{
		"CreateAccount": true,
		"TitleId":       s.conf.PlayFabTitle,
		"XboxToken":     x.Header(),
	}, nil, false, &resp); err != nil {
		return "", err
	}
	if resp.Data.SessionTicket == "" {
		return "", errors.New("auth: PlayFab login without a session ticket")
	}
	until := resp.Data.EntityToken.TokenExpiration
	if until.IsZero() {
		until = s.now().Add(time.Hour)
	}
	s.playfab = token{resp.Data.SessionTicket, until}
	return resp.Data.SessionTicket, nil
}

// startSession asks the Minecraft services for an MCToken with a PlayFab
// session ticket.
func (s *Session) startSession(ctx context.Context, ticket string) (*MCToken, error) {
	overrides := s.conf.TreatmentOverrides
	if overrides == nil {
		overrides = []string{}
	}
	var resp struct {
		Result struct {
			AuthorizationHeader string    `json:"authorizationHeader"`
			ValidUntil          time.Time `json:"validUntil"`
		} `json:"result"`
	}
	if err := s.post(ctx, s.ep.Minecraft+"/api/v1.0/session/start", map[string]any{
		"device": map[string]any{
			"applicationType":    "MinecraftPE",
			"capabilities":       []string{},
			"gameVersion":        s.conf.GameVersion,
			"id":                 newUUID(),
			"memory":             "17179869184",
			"platform":           "Windows10",
			"playFabTitleId":     s.conf.PlayFabTitle,
			"storePlatform":      "uwp.store",
			"treatmentOverrides": overrides,
			"type":               "Windows10",
		},
		"user": map[string]any{
			"language":     "en",
			"languageCode": "en-US",
			"regionCode":   "US",
			"token":        ticket,
			"tokenType":    "PlayFab",
		},
	}, nil, false, &resp); err != nil {
		return nil, err
	}
	header := resp.Result.AuthorizationHeader
	pmid, err := playerMessagingID(header)
	if err != nil {
		return nil, err
	}
	until := resp.Result.ValidUntil
	if until.IsZero() {
		return nil, errors.New("auth: MCToken without an expiry")
	}
	return &MCToken{Header: header, ValidUntil: until, PlayerMessagingID: pmid}, nil
}

// playerMessagingID reads the pmid claim of an MCToken, which came from
// the service over TLS: it is read, not verified.
func playerMessagingID(header string) (string, error) {
	jwt, ok := strings.CutPrefix(header, "MCToken ")
	if !ok {
		return "", errors.New("auth: authorization header is not an MCToken")
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return "", errors.New("auth: MCToken is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("auth: MCToken payload is not base64")
	}
	var claims struct {
		PMID string `json:"pmid"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.PMID == "" {
		return "", errors.New("auth: MCToken without a player messaging ID")
	}
	return claims.PMID, nil
}

// MultiplayerToken returns a multiplayer token of the user bound to the
// public key of a client: a JWT of the authorization service naming the
// player, which servers of the game verify with the keys of the service,
// and which the client logs in with, signing its client data with the key.
func (s *Session) MultiplayerToken(ctx context.Context, pub *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	mc, err := s.MCToken(ctx)
	if err != nil {
		return "", err
	}
	var resp struct {
		Result struct {
			SignedToken string `json:"signedToken"`
		} `json:"result"`
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.post(ctx, s.ep.Minecraft+"/api/v1.0/multiplayer/session/start", map[string]any{
		// The key becomes the cpk claim of the token.
		"publicKey": base64.StdEncoding.EncodeToString(der),
	}, http.Header{"Authorization": {mc.Header}, "Accept": {"application/json"}}, false, &resp); err != nil {
		return "", err
	}
	if strings.Count(resp.Result.SignedToken, ".") != 2 {
		return "", errors.New("auth: multiplayer token is not a JWT")
	}
	return resp.Result.SignedToken, nil
}
