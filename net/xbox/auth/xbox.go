package auth

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// xblToken is a token of the Xbox authentication services.
type xblToken struct {
	Token         string    `json:"Token"`
	NotAfter      time.Time `json:"NotAfter"`
	DisplayClaims struct {
		XUI []struct {
			UserHash string `json:"uhs"`
			XUID     string `json:"xid"`
			Gamertag string `json:"gtg"`
		} `json:"xui"`
	} `json:"DisplayClaims"`
}

func (t *xblToken) valid() bool { return t != nil && t.Token != "" && !t.NotAfter.IsZero() }

// xsts returns the token as an XSTSToken.
func (t *xblToken) xsts() (*XSTSToken, error) {
	if !t.valid() || len(t.DisplayClaims.XUI) == 0 || t.DisplayClaims.XUI[0].UserHash == "" {
		return nil, errors.New("auth: XSTS token without a user")
	}
	u := t.DisplayClaims.XUI[0]
	return &XSTSToken{Token: t.Token, UserHash: u.UserHash, XUID: u.XUID, Gamertag: u.Gamertag, NotAfter: t.NotAfter}, nil
}

// contract is the header of the requests of the Xbox token services.
var contract = http.Header{"X-Xbl-Contract-Version": {"1"}}

// deviceToken returns the token of the device, proving the proof key.
func (s *Session) deviceToken(ctx context.Context) (string, error) {
	if s.device.fresh(s.now()) {
		return s.device.value, nil
	}
	var t xblToken
	if err := s.post(ctx, s.ep.Device+"/device/authenticate", map[string]any{
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
		"Properties": map[string]any{
			"AuthMethod": "ProofOfPossession",
			"Id":         s.deviceID,
			"DeviceType": s.conf.DeviceType,
			"Version":    s.conf.DeviceVersion,
			"ProofKey":   jwk(s.key),
		},
	}, contract, true, &t); err != nil {
		return "", err
	}
	if !t.valid() {
		return "", errors.New("auth: device token missing")
	}
	s.device = token{t.Token, t.NotAfter}
	return t.Token, nil
}

// authorize signs the user in with the SISU authorization, which returns
// the user and title tokens and the XSTS token of Xbox Live.
func (s *Session) authorize(ctx context.Context) error {
	if s.user.fresh(s.now()) && s.title.fresh(s.now()) {
		if t := s.xsts[XboxLive]; t != nil && s.now().Add(refreshMargin).Before(t.NotAfter) {
			return nil
		}
	}
	device, err := s.deviceToken(ctx)
	if err != nil {
		return err
	}
	access, err := s.accessToken(ctx)
	if err != nil {
		return err
	}
	var resp struct {
		UserToken          *xblToken `json:"UserToken"`
		TitleToken         *xblToken `json:"TitleToken"`
		AuthorizationToken *xblToken `json:"AuthorizationToken"`
	}
	if err := s.post(ctx, s.ep.SISU+"/authorize", map[string]any{
		"AccessToken":       "t=" + access,
		"AppId":             s.conf.ClientID,
		"DeviceToken":       device,
		"ProofKey":          jwk(s.key),
		"RelyingParty":      XboxLive,
		"Sandbox":           s.conf.Sandbox,
		"SiteName":          "user.auth.xboxlive.com",
		"UseModernGamertag": true,
	}, nil, true, &resp); err != nil {
		return err
	}
	if !resp.UserToken.valid() || !resp.TitleToken.valid() {
		return errors.New("auth: authorization without user or title token")
	}
	x, err := resp.AuthorizationToken.xsts()
	if err != nil {
		return err
	}
	s.user = token{resp.UserToken.Token, resp.UserToken.NotAfter}
	s.title = token{resp.TitleToken.Token, resp.TitleToken.NotAfter}
	s.xsts[XboxLive] = x
	return nil
}

// xstsLocked returns the XSTS token of a relying party, asking for one
// with the device, title and user tokens if none is fresh.
func (s *Session) xstsLocked(ctx context.Context, relyingParty string) (*XSTSToken, error) {
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	if t := s.xsts[relyingParty]; t != nil && s.now().Add(refreshMargin).Before(t.NotAfter) {
		return t, nil
	}
	device, err := s.deviceToken(ctx)
	if err != nil {
		return nil, err
	}
	var t xblToken
	if err := s.post(ctx, s.ep.XSTS+"/xsts/authorize", map[string]any{
		"RelyingParty": relyingParty,
		"TokenType":    "JWT",
		"Properties": map[string]any{
			"SandboxId":   s.conf.Sandbox,
			"DeviceToken": device,
			"TitleToken":  s.title.value,
			"UserTokens":  []string{s.user.value},
		},
	}, contract, true, &t); err != nil {
		return nil, err
	}
	x, err := t.xsts()
	if err != nil {
		return nil, err
	}
	s.xsts[relyingParty] = x
	return x, nil
}
