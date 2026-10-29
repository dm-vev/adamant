package social

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFriendsAndInvites(t *testing.T) {
	var invited map[string]any
	limited := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "XBL3.0 x=1;t" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/users/me/people/friendrequests(received)":
			if r.Header.Get("X-Xbl-Contract-Version") != "7" {
				http.Error(w, "contract", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"people":[{"xuid":"2535400000000002","gamertag":"Friend"}]}`)
		case r.Method == http.MethodPut && r.URL.Path == "/users/me/people/friends/v2/xuid(2535400000000002)":
			if limited {
				limited = false
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = io.WriteString(w, `{"isFriend":true}`)
		case r.Method == http.MethodPost && r.URL.Path == "/handles":
			_ = json.NewDecoder(r.Body).Decode(&invited)
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := Client{Token: func(context.Context) (string, error) { return "XBL3.0 x=1;t", nil }, Endpoints: Endpoints{srv.URL, srv.URL, srv.URL}}
	ctx := context.Background()
	people, err := c.FriendRequests(ctx)
	if err != nil || len(people) != 1 || people[0].Gamertag != "Friend" {
		t.Fatalf("requests %+v, %v", people, err)
	}
	var rl *RateLimited
	if _, err := c.AddFriend(ctx, people[0].XUID); !errors.As(err, &rl) || rl.After.Seconds() != 7 {
		t.Fatalf("rate limit came back as %v", err)
	}
	if friend, err := c.AddFriend(ctx, people[0].XUID); err != nil || !friend {
		t.Fatalf("added %v, %v", friend, err)
	}
	if _, err := c.AddFriend(ctx, "1)/../../x"); err == nil {
		t.Fatal("a malformed XUID went into a URL")
	}
	ref := map[string]any{"scid": "s", "templateName": "MinecraftLobby", "name": "N"}
	if err := c.Invite(ctx, people[0].XUID, ref); err != nil {
		t.Fatal(err)
	}
	if invited["type"] != "invite" || invited["invitedXuid"] != "2535400000000002" || invited["inviteAttributes"].(map[string]any)["titleId"] != TitleID {
		t.Fatalf("invite %v", invited)
	}
}
