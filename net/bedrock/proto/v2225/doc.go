// Package v2225 holds the packets of Bedrock protocol 2225 (1.26.60 and
// 1.26.61) and their codecs, generated from schema.txt, which protoimport
// writes from the codecs of gophertunnel (net/interop/v2225). Codecs the
// schema cannot describe are written by hand in the hand*.go files, those
// of protocol 2193 with what 2225 changed of them. A packet whose codec is
// not complete keeps its fields as bytes; Raw reports it.
package v2225

//go:generate go run ../gen -schema schema.txt -o packets.go -pkg v2225

// Protocol and Version are the number of the protocol and the version of
// the game it is of.
const (
	Protocol = 2225
	Version  = "1.26.60"
)
