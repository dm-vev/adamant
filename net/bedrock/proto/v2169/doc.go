// Package v2169 holds the packets of Bedrock protocol 2169 (1.26.45) and
// their codecs, generated from schema.txt, which protoimport writes from
// the codecs of gophertunnel v1.61.0, corrected where they differ from
// Mojang's protocol documentation. Codecs the schema cannot describe are
// written by hand in the hand*.go files. A packet whose codec is not
// complete keeps its fields as bytes; Raw reports it.
package v2169

//go:generate go run ../gen -schema schema.txt -o packets.go -pkg v2169

// Protocol and Version are the number of the protocol and the version of
// the game it is of.
const (
	Protocol = 2169
	Version  = "1.26.45"
)
