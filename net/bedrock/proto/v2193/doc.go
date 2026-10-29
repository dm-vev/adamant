// Package v2193 holds the packets of Bedrock protocol 2193 (1.26.50) and
// their codecs, generated from schema.txt, which protoimport writes from
// the codecs of gophertunnel. Codecs the schema cannot describe are
// written by hand in the hand*.go files. A packet whose codec is not
// complete keeps its fields as bytes; Raw reports it.
package v2193

//go:generate go run ../gen -schema schema.txt -o packets.go -pkg v2193

// Protocol and Version are the number of the protocol and the version of
// the game it is of.
const (
	Protocol = 2193
	Version  = "1.26.50"
)
