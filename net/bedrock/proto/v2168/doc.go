// Package v2168 holds the packets of Bedrock protocol 2168 (1.26.40 to
// 1.26.44) and their codecs, generated from schema.txt, which protoimport
// writes from the codecs of gophertunnel v1.59.0, corrected where they
// differ from Mojang's protocol documentation. Codecs the schema cannot
// describe are written by hand in the hand*.go files. A packet whose codec
// is not complete keeps its fields as bytes; Raw reports it.
package v2168

//go:generate go run ../gen -schema schema.txt -o packets.go -pkg v2168

// Protocol and Version are the number of the protocol and the version of
// the game it is of.
const (
	Protocol = 2168
	Version  = "1.26.40"
)
