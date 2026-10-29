// Package nbt implements the NBT formats used by Minecraft Bedrock Edition and Minecraft Java Edition. These
// formats are a little endian format, a big endian format and a little endian format using varints (typically
// used over network in Bedrock Edition).
//
// The package exposes serialisation and deserialisation roughly the same way as the JSON standard library
// does, using nbt.Marshal() and nbt.Unmarshal when working with byte slices, and nbt.NewEncoder() and
// nbt.NewDecoder() when working with readers or writers.
//
// The package encodes and decodes the following Go types with the following NBT tags.
//
//	byte/uint8: TAG_Byte
//	bool: TAG_Byte
//	int16: TAG_Short
//	int32: TAG_Int
//	int64: TAG_Long
//	float32: TAG_Float
//	float64: TAG_Double
//	[...]byte: TAG_ByteArray
//	[...]int32: TAG_IntArray
//	[...]int64: TAG_LongArray
//	string: TAG_String
//	[]<type>: TAG_List
//	struct{...}: TAG_Compound
//	map[string]<type/any>: TAG_Compound
//
// Structures decoded or encoded may have struct field tags in a comparable way to the JSON standard library.
// The 'nbt' struct tag may be filled out the following ways:
//
//	'-': Ignores the field completely when encoding and decoding.
//	',omitempty': Doesn't encode the field if its value is the same as the default value.
//	'name(,omitempty)': Encodes/decodes the field with a different name than its usual name.
//
// If no 'nbt' struct tag is present for a field, the name of the field will be used to encode/decode the
// struct. Note that this package, unlike the JSON standard library package, is case sensitive when decoding.
//
// The package comes from the nbt package of gophertunnel v1.62.0
// (github.com/sandertv/gophertunnel), MIT licensed, Copyright (c) 2019
// Sandertv: see LICENSE.gophertunnel. The server keeps its worlds,
// items and structures in its format, which must stay byte for byte as
// it was, so the codec is taken as it is rather than written again, but
// for its decoder, which no longer trusts the data: a length allocates
// no more than the data that follows it, lists nest no deeper than
// compounds, a list of numbers leaves the depth as it found it, and a
// value that cannot hold what the data has is an error rather than a
// panic in reflect.
package nbt
