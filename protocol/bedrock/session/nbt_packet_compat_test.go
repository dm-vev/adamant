package session

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/go-gl/mathgl/mgl64"
)

func TestBlockActorDataNBTBytes(t *testing.T) {
	t.Parallel()
	pk := &mc.BlockActorDataPacket{
		Position: mc.BlockPos{12, 64, -7},
		NBTData:  block.EnchantingTable{}.EncodeNBT(),
	}
	got := marshalPacketBytes(t, pk)
	want, err := hex.DecodeString("381880010d0a00080269640c456e6368616e745461626c650504726f74740000000000")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("BlockActorData bytes differ\ngot:  %x\nwant: %x", got, want)
	}
}

func TestEndCrystalMetadataPacket(t *testing.T) {
	t.Parallel()
	m := mc.NewMetadata()
	(&Session{}).addSpecificMetadata(endCrystalMetadataStub{
		showBase:  true,
		target:    mgl64.Vec3{12.9, 64.1, -7.1},
		hasTarget: true,
	}, m)

	if got, want := m[mc.EntityDataKeyBlockTarget], (mc.BlockPos{12, 64, -8}); got != want {
		t.Fatalf("block target = %#v, want %#v", got, want)
	}
	if !m.Flag(mc.EntityDataKeyFlags, mc.EntityDataFlagShowBottom) {
		t.Fatal("show-bottom flag is not set")
	}
	for _, key := range []uint32{mc.EntityDataKeyTargetA, mc.EntityDataKeyTargetB, mc.EntityDataKeyTargetC} {
		if _, ok := m[key]; ok {
			t.Fatalf("legacy target metadata key %d is set", key)
		}
	}
	if got := marshalPacketBytes(t, &mc.SetActorDataPacket{EntityRuntimeID: 1, EntityMetadata: mc.Metadata(m)}); len(got) == 0 {
		t.Fatal("empty SetActorData packet")
	}

	withoutTarget := mc.NewMetadata()
	(&Session{}).addSpecificMetadata(endCrystalMetadataStub{}, withoutTarget)
	if _, ok := withoutTarget[mc.EntityDataKeyBlockTarget]; ok {
		t.Fatal("nil beam target was encoded")
	}
}

type endCrystalMetadataStub struct {
	showBase  bool
	target    mgl64.Vec3
	hasTarget bool
}

func (e endCrystalMetadataStub) ShowBase() bool { return e.showBase }
func (e endCrystalMetadataStub) BeamTarget() (mgl64.Vec3, bool) {
	return e.target, e.hasTarget
}

// encodePacket encodes the fields of a packet of the generated codecs.
func encodePacket(pk interface{ Encode(*wire.Writer) }) []byte {
	var w wire.Writer
	pk.Encode(&w)
	return w.B
}

func marshalPacketBytes(t *testing.T, pk proto.Packet) []byte {
	t.Helper()
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()})}
	pk.Encode(&w)
	return w.B
}
