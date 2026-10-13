package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/df-mc/go-nethernet"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// bindOfflineIdentity binds the SDP fingerprint assertion and Minecraft Login to
// one locally generated key. No account token or authenticated XUID is involved.
func bindOfflineIdentity(dialer *minecraft.Dialer, network *minecraft.NetherNet) error {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate offline identity: %w", err)
	}
	dialer.IdentityData.Identity = uuid.NewString()
	request := login.EncodeOffline(dialer.IdentityData, login.ClientData{}, key, false)
	// EncodeOffline returns a length-prefixed JSON certificate followed by the
	// client-data JWT. Reuse its token rather than duplicate the offline claims.
	var certificate struct {
		Token string
	}
	length := binary.LittleEndian.Uint32(request[:4])
	if err := json.Unmarshal(request[4:4+length], &certificate); err != nil {
		return fmt.Errorf("extract offline identity token: %w", err)
	}
	network.Dialer.Identity = &nethernet.Identity{
		PrivateKey: key,
		Token:      certificate.Token,
	}
	dialer.Protocol = offlineLoginProtocol{Protocol: minecraft.DefaultProtocol, key: key}
	return nil
}

// Gophertunnel generates its own login key and only passes it to the transport
// for online authentication. NetherNet already encrypts traffic with DTLS and
// disables Minecraft packet encryption, so only Login needs the shared key.
// Keep this adapter restricted to the offline NetherNet probe.
type offlineLoginProtocol struct {
	minecraft.Protocol
	key *ecdsa.PrivateKey
}

func (p offlineLoginProtocol) ConvertFromLatest(pk packet.Packet, conn *minecraft.Conn) []packet.Packet {
	if request, ok := pk.(*packet.Login); ok {
		request.ConnectionRequest = login.EncodeOffline(conn.IdentityData(), conn.ClientData(), p.key, false)
	}
	return []packet.Packet{pk}
}
