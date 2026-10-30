package login

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKeysReadyBeforeLogin(t *testing.T) {
	s := newAuthService(t)
	keys, err := NewKeys(context.Background(), nil, s.URL)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	token := signRS256(t, s.key, s.kid, s.claims(now, "client-key"))
	s.Close()
	if _, err := keys.verify(context.Background(), token, now); err != nil {
		t.Fatalf("login needs the authorization service after initialization: %v", err)
	}
}

func TestNewKeysFetchFailure(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "unavailable"
		if cancelled {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/.well-known/openid-configuration" {
					_, _ = w.Write([]byte(`{"jwks_uri":"http://` + r.Host + `/keys"}`))
					return
				}
				if cancelled {
					cancel()
					<-r.Context().Done()
					return
				}
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			}))
			defer s.Close()
			keys, err := NewKeys(ctx, nil, s.URL)
			if err == nil || keys != nil {
				t.Fatalf("returned uninitialized keys: %v, %v", keys, err)
			}
			if !strings.Contains(err.Error(), "keys of the authorization service") {
				t.Fatalf("missing fetch context: %v", err)
			}
			if cancelled && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}
