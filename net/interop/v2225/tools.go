//go:build tools

package v2225

// constimport loads the codec package it writes the constants of, which
// keeps the net module required here.
import _ "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
