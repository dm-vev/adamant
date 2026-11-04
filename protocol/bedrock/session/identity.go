package session

import (
	"github.com/df-mc/dragonfly/server/client"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/google/uuid"
)

// IdentityOf returns the identity of a Bedrock login, which Xbox Live
// vouched for if authenticated. The UUID is uuid.Nil if the login holds
// none that parses.
func IdentityOf(id login.Identity, authenticated bool) client.Identity {
	u, err := uuid.Parse(id.Identity)
	if err != nil {
		u = uuid.Nil
	}
	return client.Identity{Name: id.DisplayName, UUID: u, XUID: id.XUID, Authenticated: authenticated}
}

// identityOf returns the identity of the login of conn: authenticated if
// conn reports, as a netconn.Conn does, that Xbox Live vouched for it.
func identityOf(conn Conn) client.Identity {
	a, ok := conn.(interface{ Authenticated() bool })
	return IdentityOf(conn.IdentityData(), ok && a.Authenticated())
}

// ProfileOf returns the profile of the client data of a Bedrock login.
func ProfileOf(d login.ClientData) client.Profile {
	return client.Profile{DeviceID: d.DeviceID, DeviceModel: d.DeviceModel, SelfSignedID: d.SelfSignedID}
}
