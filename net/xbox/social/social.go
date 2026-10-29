// Package social takes the friend requests of the account a game is
// hosted as on Xbox Live and invites the new friends to the game: the
// friends of the host see its game and join it from their friends list.
package social

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Endpoints are the services a Client talks to.
type Endpoints struct {
	Social, PeopleHub, SessionDirectory string
}

// DefaultEndpoints are the services of Xbox Live.
var DefaultEndpoints = Endpoints{
	Social:           "https://social.xboxlive.com",
	PeopleHub:        "https://peoplehub.xboxlive.com",
	SessionDirectory: "https://sessiondirectory.xboxlive.com",
}

// TitleID is the title of the game the invites are of: Minecraft for
// Windows.
const TitleID = "896928775"

// Client talks to the social services of Xbox Live as the account of the
// host.
type Client struct {
	// Token returns the XSTS token of Xbox Live as an Authorization header.
	Token func(ctx context.Context) (string, error)
	// Endpoints are the services. Zero values mean DefaultEndpoints.
	Endpoints Endpoints
	// HTTPClient sends requests. Nil means http.DefaultClient.
	HTTPClient *http.Client
}

// Person is someone of the people of the account.
type Person struct {
	XUID     string `json:"xuid"`
	Gamertag string `json:"gamertag"`
}

// RateLimited is the error of a request the service refused for the
// account sent too many: After tells when to try again.
type RateLimited struct{ After time.Duration }

func (e *RateLimited) Error() string { return fmt.Sprintf("social: rate limited for %v", e.After) }

// maxResponse bounds the body of a response read, maxPeople the people
// taken of one.
const (
	maxResponse = 1 << 20
	maxPeople   = 1000
)

// FriendRequests returns the people who sent the account a friend request.
func (c Client) FriendRequests(ctx context.Context) ([]Person, error) {
	var resp struct {
		People []Person `json:"people"`
	}
	if _, err := c.do(ctx, http.MethodGet, c.endpoints().PeopleHub+"/users/me/people/friendrequests(received)", "7", nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.People) > maxPeople {
		resp.People = resp.People[:maxPeople]
	}
	return resp.People, nil
}

// AddFriend adds a person as a friend of the account: it takes the friend
// request of the person, or sends one. It reports whether they are friends
// now.
func (c Client) AddFriend(ctx context.Context, xuid string) (bool, error) {
	if !validXUID(xuid) {
		return false, errors.New("social: malformed XUID")
	}
	var resp struct {
		IsFriend bool `json:"isFriend"`
	}
	_, err := c.do(ctx, http.MethodPut, c.endpoints().Social+"/users/me/people/friends/v2/xuid("+xuid+")", "", nil, &resp)
	return resp.IsFriend, err
}

// Invite invites a friend to the session of a reference, as mpsd.Host.Ref
// returns it.
func (c Client) Invite(ctx context.Context, xuid string, session map[string]any) error {
	if !validXUID(xuid) {
		return errors.New("social: malformed XUID")
	}
	_, err := c.do(ctx, http.MethodPost, c.endpoints().SessionDirectory+"/handles", "107", map[string]any{
		"version":          1,
		"type":             "invite",
		"sessionRef":       session,
		"invitedXuid":      xuid,
		"inviteAttributes": map[string]string{"titleId": TitleID},
	}, nil)
	return err
}

// validXUID reports whether s is a XUID: decimal digits, as many as a
// uint64 has.
func validXUID(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil && len(s) <= 20
}

func (c Client) endpoints() Endpoints {
	e, d := c.Endpoints, DefaultEndpoints
	if e.Social == "" {
		e.Social = d.Social
	}
	if e.PeopleHub == "" {
		e.PeopleHub = d.PeopleHub
	}
	if e.SessionDirectory == "" {
		e.SessionDirectory = d.SessionDirectory
	}
	return e
}

// do sends a request and decodes the JSON response into out.
func (c Client) do(ctx context.Context, method, url, contract string, body, out any) (int, error) {
	auth, err := c.Token(ctx)
	if err != nil {
		return 0, err
	}
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept-Language", "en-GB")
	if contract != "" {
		req.Header.Set("X-Xbl-Contract-Version", contract)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return resp.StatusCode, err
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		after, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return resp.StatusCode, &RateLimited{After: time.Duration(max(after, 1)) * time.Second}
	case resp.StatusCode/100 != 2:
		if len(data) > 256 {
			data = data[:256]
		}
		return resp.StatusCode, fmt.Errorf("social: %s %s: status %d: %s", method, req.URL.Path, resp.StatusCode, data)
	}
	if out != nil && len(data) > 0 {
		return resp.StatusCode, json.Unmarshal(data, out)
	}
	return resp.StatusCode, nil
}
