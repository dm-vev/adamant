package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// DeviceCode is a login waiting for the user to enter a code at a page of
// Microsoft.
type DeviceCode struct {
	// UserCode is the code the user enters at VerificationURI.
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time

	conf     Config
	code     string
	interval time.Duration
}

// StartLogin starts signing an account in with the device code flow: the
// user enters the code returned at its page, and Wait returns the Session
// once they did.
func (c Config) StartLogin(ctx context.Context) (*DeviceCode, error) {
	var resp struct {
		UserCode        string `json:"user_code"`
		DeviceCode      string `json:"device_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int64  `json:"expires_in"`
		Interval        int64  `json:"interval"`
		Error           string `json:"error"`
	}
	status, err := c.form(ctx, "/oauth20_connect.srf", url.Values{
		"client_id":     {c.ClientID},
		"scope":         {scope},
		"response_type": {"device_code"},
	}, &resp)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || resp.DeviceCode == "" || resp.UserCode == "" {
		return nil, fmt.Errorf("auth: device code refused: status %d %s", status, resp.Error)
	}
	interval := time.Duration(resp.Interval) * time.Second
	if interval <= 0 || interval > time.Minute {
		interval = 5 * time.Second
	}
	return &DeviceCode{
		UserCode:        resp.UserCode,
		VerificationURI: resp.VerificationURI,
		ExpiresAt:       expiry(time.Now(), resp.ExpiresIn),
		conf:            c,
		code:            resp.DeviceCode,
		interval:        interval,
	}, nil
}

// msaToken is a token response of the Microsoft account service.
type msaToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

// Wait polls for the login until the user entered the code, it expired or
// the context ended, and returns the Session of the account.
func (d *DeviceCode) Wait(ctx context.Context) (*Session, error) {
	interval := d.interval
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		var resp msaToken
		if _, err := d.conf.form(ctx, "/oauth20_token.srf", url.Values{
			"client_id":   {d.conf.ClientID},
			"device_code": {d.code},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}, &resp); err != nil {
			return nil, err
		}
		switch resp.Error {
		case "":
			if resp.AccessToken == "" || resp.RefreshToken == "" {
				return nil, errors.New("auth: login without tokens")
			}
			return d.conf.newSession(token{resp.AccessToken, expiry(time.Now(), resp.ExpiresIn)}, resp.RefreshToken)
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		default:
			return nil, fmt.Errorf("auth: login failed: %s", resp.Error)
		}
	}
}

// accessToken returns the access token of the Microsoft account, renewing
// it with the refresh token, which the service replaces.
func (s *Session) accessToken(ctx context.Context) (string, error) {
	if s.access.fresh(time.Now()) {
		return s.access.value, nil
	}
	var resp msaToken
	status, err := s.conf.form(ctx, "/oauth20_token.srf", url.Values{
		"client_id":     {s.conf.ClientID},
		"scope":         {scope},
		"grant_type":    {"refresh_token"},
		"refresh_token": {s.refresh},
	}, &resp)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK || resp.AccessToken == "" {
		return "", fmt.Errorf("auth: refresh refused: status %d %s", status, resp.Error)
	}
	s.access = token{resp.AccessToken, expiry(time.Now(), resp.ExpiresIn)}
	if resp.RefreshToken != "" && resp.RefreshToken != s.refresh {
		s.refresh = resp.RefreshToken
		if s.onRefresh != nil {
			s.onRefresh(s.snapshot())
		}
	}
	return s.access.value, nil
}
