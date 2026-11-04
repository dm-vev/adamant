// Package v2225 checks the codecs of Bedrock protocol 2225 (1.26.60) of
// the net module against those of gophertunnel, as package interop checks
// those of 2193.
//
// It is a module of its own: interop pins gophertunnel v1.62.0, which
// speaks 2193, and this module pins the gophertunnel that speaks 2225,
// master at 0e179fc4 (2026-10-30) or later, which has the 1.26.60 layouts
// of #545. The commands of interop run against 2225 from here:
//
//	cd net/interop/v2225
//	go run github.com/dm-vev/adamant/net/interop/cmd/protoimport -o ../../bedrock/proto/v2225/schema.txt
//	go run github.com/dm-vev/adamant/net/interop/cmd/constimport -pkg v2225 -o ../../bedrock/proto/v2225/consts.go
package v2225
