package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/google/uuid"
)

func TestIdentityOf(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-00000000901d")
	for name, tc := range map[string]struct {
		in   login.Identity
		want client.Identity
	}{
		"xbox live": {
			in:   login.Identity{DisplayName: "Golden", Identity: id.String(), XUID: "2535400000000042", PlayFabID: "p", TitleID: "t"},
			want: client.Identity{Name: "Golden", UUID: id, XUID: "2535400000000042"},
		},
		"offline": {
			in:   login.Identity{DisplayName: "Steve", Identity: id.String()},
			want: client.Identity{Name: "Steve", UUID: id},
		},
		"name kept as it is": {
			in:   login.Identity{DisplayName: " Golden ", Identity: id.String()},
			want: client.Identity{Name: " Golden ", UUID: id},
		},
		"uuid in upper case": {
			in:   login.Identity{DisplayName: "Golden", Identity: "00000000-0000-4000-8000-00000000901D"},
			want: client.Identity{Name: "Golden", UUID: id},
		},
		"no uuid": {
			in:   login.Identity{DisplayName: "Golden"},
			want: client.Identity{Name: "Golden", UUID: uuid.Nil},
		},
		"uuid that does not parse": {
			in:   login.Identity{DisplayName: "Golden", Identity: "not-a-uuid", XUID: "1"},
			want: client.Identity{Name: "Golden", UUID: uuid.Nil, XUID: "1"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := IdentityOf(tc.in); got != tc.want {
				t.Fatalf("IdentityOf(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestProfileOf(t *testing.T) {
	for name, tc := range map[string]struct {
		in   login.ClientData
		want client.Profile
	}{
		"all told": {
			in:   login.ClientData{DeviceID: "device", DeviceModel: "model", SelfSignedID: "self", LanguageCode: "en_GB"},
			want: client.Profile{DeviceID: "device", DeviceModel: "model", SelfSignedID: "self"},
		},
		"none told": {},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ProfileOf(tc.in); got != tc.want {
				t.Fatalf("ProfileOf = %+v, want %+v", got, tc.want)
			}
		})
	}
}
