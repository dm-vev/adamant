package interop

import (
	"testing"

	"github.com/df-mc/go-nethernet/discovery"
	"github.com/dm-vev/adamant/net/nethernet/lan"
)

// TestLANPacketsMatchGoNetherNet decodes the LAN discovery packets of
// go-nethernet, which joins the game's LAN, with ours and the other way
// round, server data included.
func TestLANPacketsMatchGoNetherNet(t *testing.T) {
	data := discovery.ServerData{ServerName: "Adamant", Protocol: 2193, Version: "1.26.50", LevelName: "world",
		GameType: discovery.GameTypeCreative, PlayerCount: 2, MaxPlayerCount: 20, Hardcore: true,
		AcceptsOnlineAuth: true, AcceptsSelfSignedAuth: true, Nonce: "nonce", ConnectionType: 4}
	theirData, err := data.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	ours := lan.ServerData{ServerName: "Adamant", Protocol: 2193, Version: "1.26.50", LevelName: "world",
		GameType: lan.GameTypeCreative, PlayerCount: 2, MaxPlayerCount: 20, Hardcore: true,
		AcceptsOnlineAuth: true, AcceptsSelfSignedAuth: true, Nonce: "nonce", ConnectionType: lan.ConnectionTypeLAN}
	ourData, _ := ours.MarshalBinary()
	if string(ourData) != string(theirData) {
		t.Fatalf("server data % x, go-nethernet % x", ourData, theirData)
	}

	// Theirs to ours.
	for _, c := range []struct {
		theirs discovery.Packet
		check  func(lan.Packet) bool
	}{
		{&discovery.RequestPacket{}, func(p lan.Packet) bool { _, ok := p.(*lan.Request); return ok }},
		{&discovery.ResponsePacket{ApplicationData: theirData}, func(p lan.Packet) bool {
			r, ok := p.(*lan.Response)
			return ok && string(r.ApplicationData) == string(theirData)
		}},
		{&discovery.MessagePacket{RecipientID: 77, Data: "CONNECTREQUEST 5 offer"}, func(p lan.Packet) bool {
			m, ok := p.(*lan.Message)
			return ok && m.RecipientID == 77 && m.Data == "CONNECTREQUEST 5 offer"
		}},
	} {
		pk, sender, err := lan.Unmarshal(discovery.Marshal(c.theirs, 1234))
		if err != nil || sender != 1234 || !c.check(pk) {
			t.Fatalf("%T read as %+v from %d, %v", c.theirs, pk, sender, err)
		}
	}
	// Ours to theirs.
	for _, pk := range []lan.Packet{&lan.Request{}, &lan.Response{ApplicationData: ourData}, &lan.Message{RecipientID: 77, Data: "CONNECTRESPONSE 5 answer"}} {
		theirs, sender, err := discovery.Unmarshal(lan.Marshal(pk, 4321))
		if err != nil || sender != 4321 {
			t.Fatalf("%T: go-nethernet read %+v from %d, %v", pk, theirs, sender, err)
		}
		switch pk := pk.(type) {
		case *lan.Response:
			if r := theirs.(*discovery.ResponsePacket); string(r.ApplicationData) != string(pk.ApplicationData) {
				t.Fatalf("response read as % x", r.ApplicationData)
			}
		case *lan.Message:
			if m := theirs.(*discovery.MessagePacket); m.RecipientID != pk.RecipientID || m.Data != pk.Data {
				t.Fatalf("message read as %+v", m)
			}
		}
	}
}
