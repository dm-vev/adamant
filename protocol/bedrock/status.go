package bedrockadapter

import (
	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/server"
)

// netconnStatusProvider returns the StatusProvider of the server as netconn
// shows it to clients of Bedrock Edition, or nil, which leaves netconn its
// default, if p is nil.
func netconnStatusProvider(p server.StatusProvider) netconn.StatusProvider {
	if p == nil {
		return nil
	}
	return netconnStatus{p}
}

// netconnStatus is a StatusProvider as netconn reads it: the Status of the
// provider, field for field, for the counts netconn passes.
type netconnStatus struct{ provider server.StatusProvider }

// ServerStatus returns the status of the provider for the counts passed.
func (s netconnStatus) ServerStatus(playerCount, maxPlayers int) netconn.ServerStatus {
	st := s.provider.ServerStatus(playerCount, maxPlayers)
	return netconn.ServerStatus{ServerName: st.Name, ServerSubName: st.SubName, PlayerCount: st.PlayerCount, MaxPlayers: st.MaxPlayers}
}
