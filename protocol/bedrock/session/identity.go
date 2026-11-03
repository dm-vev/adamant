package session

import (
	"github.com/df-mc/dragonfly/server/client"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/google/uuid"
)

// IdentityOf returns the identity of a Bedrock login. The UUID is uuid.Nil
// if the login holds none that parses.
func IdentityOf(id login.Identity) client.Identity {
	u, err := uuid.Parse(id.Identity)
	if err != nil {
		u = uuid.Nil
	}
	return client.Identity{Name: id.DisplayName, UUID: u, XUID: id.XUID}
}

// ProfileOf returns the profile of the client data of a Bedrock login.
func ProfileOf(d login.ClientData) client.Profile {
	return client.Profile{DeviceID: d.DeviceID, DeviceModel: d.DeviceModel, SelfSignedID: d.SelfSignedID}
}
