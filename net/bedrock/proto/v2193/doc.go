// Package v2193 holds the packets of Bedrock protocol 2193 (1.26.50) and
// their codecs, generated from schema.txt. The schema is the source of
// truth: it was seeded from gophertunnel by protoimport and is edited by
// hand from there. Packets whose codec is not complete yet keep their
// fields as bytes; Raw reports them.
package v2193

//go:generate go run ../gen -schema schema.txt -o packets.go -pkg v2193
