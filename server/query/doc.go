// Package query implements the Bedrock query protocol for Adamant servers.
//
// The package exposes a provider interface that allows the main server
// implementation to describe its current state. A Responder answers the
// query requests a listener hands it from its UDP socket using the data
// supplied by that provider.
package query
