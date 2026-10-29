package proto

import (
	"github.com/dm-vev/adamant/net/bedrock/nbt"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// The codecs of the composite wire types of the schema, which generated
// code calls.

// WriteString16 writes a string prefixed with its length as an int16.
func WriteString16(w *wire.Writer, v *string) {
	w.Int16(int16(len(*v)))
	w.Bytes([]byte(*v))
}

func ReadString16(r *wire.Reader, v *string) {
	n := int(r.Int16())
	if n < 0 {
		r.Fail(wire.ErrShort)
		return
	}
	*v = string(r.Next(n))
}

// WriteRest writes bytes as the end of a packet.
func WriteRest(w *wire.Writer, v *[]byte) { w.Bytes(*v) }

// ReadRest reads the rest of a packet.
func ReadRest(r *wire.Reader, v *[]byte) { *v = append([]byte(nil), r.Bytes()...) }

func WriteUUID(w *wire.Writer, v *[16]byte) { w.UUID(*v) }
func ReadUUID(r *wire.Reader, v *[16]byte)  { *v = r.UUID() }

func WriteVec2(w *wire.Writer, v *[2]float32) {
	w.Float32(v[0])
	w.Float32(v[1])
}

func ReadVec2(r *wire.Reader, v *[2]float32) {
	v[0], v[1] = r.Float32(), r.Float32()
}

func WriteVec3(w *wire.Writer, v *[3]float32) {
	w.Float32(v[0])
	w.Float32(v[1])
	w.Float32(v[2])
}

func ReadVec3(r *wire.Reader, v *[3]float32) {
	v[0], v[1], v[2] = r.Float32(), r.Float32(), r.Float32()
}

// WriteRGB writes a colour as three float32s from 0 to 1.
func WriteRGB(w *wire.Writer, v *[3]float32) { WriteVec3(w, v) }
func ReadRGB(r *wire.Reader, v *[3]float32)  { ReadVec3(r, v) }

// WriteSoundPos writes a position as a block position in eighths of a
// block.
func WriteSoundPos(w *wire.Writer, v *[3]float32) {
	w.Varint32(int32(v[0] * 8))
	w.Varint32(int32(v[1] * 8))
	w.Varint32(int32(v[2] * 8))
}

func ReadSoundPos(r *wire.Reader, v *[3]float32) {
	v[0] = float32(r.Varint32()) / 8
	v[1] = float32(r.Varint32()) / 8
	v[2] = float32(r.Varint32()) / 8
}

func WriteBlockPos(w *wire.Writer, v *[3]int32) {
	w.Varint32(v[0])
	w.Varint32(v[1])
	w.Varint32(v[2])
}

func ReadBlockPos(r *wire.Reader, v *[3]int32) {
	v[0], v[1], v[2] = r.Varint32(), r.Varint32(), r.Varint32()
}

func WriteChunkPos(w *wire.Writer, v *[2]int32) {
	w.Varint32(v[0])
	w.Varint32(v[1])
}

func ReadChunkPos(r *wire.Reader, v *[2]int32) {
	v[0], v[1] = r.Varint32(), r.Varint32()
}

func WriteSubChunkPos(w *wire.Writer, v *[3]int32) {
	w.Int32(v[0])
	w.Int32(v[1])
	w.Int32(v[2])
}

func ReadSubChunkPos(r *wire.Reader, v *[3]int32) {
	v[0], v[1], v[2] = r.Int32(), r.Int32(), r.Int32()
}

// WriteRGBA writes a colour as a little-endian uint32, red in the low byte.
func WriteRGBA(w *wire.Writer, v *Color) {
	w.Uint32(uint32(v[0]) | uint32(v[1])<<8 | uint32(v[2])<<16 | uint32(v[3])<<24)
}

func ReadRGBA(r *wire.Reader, v *Color) {
	x := r.Uint32()
	*v = Color{byte(x), byte(x >> 8), byte(x >> 16), byte(x >> 24)}
}

// WriteARGBBE writes a colour as a big-endian int32, alpha in the low byte.
func WriteARGBBE(w *wire.Writer, v *Color) {
	w.Int32BE(int32(v[3]) | int32(v[0])<<8 | int32(v[1])<<16 | int32(v[2])<<24)
}

func ReadARGBBE(r *wire.Reader, v *Color) {
	x := r.Int32BE()
	*v = Color{byte(x >> 8), byte(x >> 16), byte(x >> 24), byte(x)}
}

// WriteNBT writes a compound as network NBT. A value of no NBT tag is a bug
// of the caller.
func WriteNBT(w *wire.Writer, v *map[string]any) {
	if err := nbt.Write(w, *v); err != nil {
		panic(err)
	}
}

func ReadNBT(r *wire.Reader, v *map[string]any) {
	m, err := nbt.Read(r)
	if err != nil {
		r.Fail(err)
		return
	}
	*v = m
}

// WriteNBTList writes a list as network NBT.
func WriteNBTList(w *wire.Writer, v *[]any) {
	if err := nbt.WriteList(w, *v); err != nil {
		panic(err)
	}
}

func ReadNBTList(r *wire.Reader, v *[]any) {
	l, err := nbt.ReadList(r)
	if err != nil {
		r.Fail(err)
		return
	}
	*v = l
}
