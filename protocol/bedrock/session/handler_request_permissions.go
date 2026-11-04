package session

import (
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/google/uuid"
)

// RequestPermissionsHandler handles the RequestPermissions packet, which a
// client sends as its player changes the role of another from its list of
// players. The server decides whether the change is made, and shows the
// clients the permissions that hold then.
type RequestPermissionsHandler struct{}

// Handle ...
func (RequestPermissionsHandler) Handle(p proto.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*mc.RequestPermissionsPacket)
	if s.conf.RequestRole == nil {
		return nil
	}
	r, ok := roleRequest(pk.PermissionLevel, pk.RequestedPermissions)
	if !ok {
		return nil
	}
	target, ok := s.playerOfUniqueID(pk.EntityUniqueID, c)
	if !ok {
		return nil
	}
	s.conf.RequestRole(c.UUID(), target, r)
	return nil
}

// playerOfUniqueID returns the UUID of the player listed of the unique ID
// the client knows it by: 1 for its own player.
func (s *Session) playerOfUniqueID(id int64, c Controllable) (uuid.UUID, bool) {
	if id == int64(selfEntityRuntimeID) {
		return c.UUID(), true
	}
	if s.conf.Roster == nil {
		return uuid.UUID{}, false
	}
	for _, p := range s.conf.Roster.Players() {
		if p.Handle.UniqueID() == id {
			return p.Handle.UUID(), true
		}
	}
	return uuid.UUID{}, false
}

// requestedAbilities are the abilities of a custom role by the bits of
// RequestPermissions that ask for them.
var requestedAbilities = []struct {
	bit uint16
	a   client.Abilities
}{
	{mc.ActionPermissionBuild, client.AbilityBuild},
	{mc.ActionPermissionMine, client.AbilityMine},
	{mc.ActionPermissionDoorsAndSwitches, client.AbilityDoorsAndSwitches},
	{mc.ActionPermissionOpenContainers, client.AbilityOpenContainers},
	{mc.ActionPermissionAttackPlayers, client.AbilityAttackPlayers},
	{mc.ActionPermissionAttackMobs, client.AbilityAttackMobs},
	{mc.ActionPermissionOperator, client.AbilityOperatorCommands},
	{mc.ActionPermissionTeleport, client.AbilityTeleport},
}

// roleRequest returns the role a RequestPermissions packet asks for: of
// its permission level, with the abilities of its bits for a custom role.
func roleRequest(level int32, bits uint16) (client.RoleRequest, bool) {
	var r client.RoleRequest
	switch level {
	case int32(mc.PermissionLevelVisitor):
		r.Role = client.RoleVisitor
	case int32(mc.PermissionLevelMember):
		r.Role = client.RoleMember
	case int32(mc.PermissionLevelOperator):
		r.Role = client.RoleOperator
	case int32(mc.PermissionLevelCustom):
		r.Role = client.RoleCustom
		for _, b := range requestedAbilities {
			if bits&b.bit != 0 {
				r.Abilities |= b.a
			}
		}
	default:
		return r, false
	}
	return r, true
}
