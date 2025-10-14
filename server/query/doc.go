// Package query implements the Bedrock query protocol for Adamant servers.
//
// The package exposes a provider interface that allows the main server
// implementation to describe its current state. The query package handles
// the RakNet-specific wiring and responds to external query requests using
// the data supplied by that provider.
package query
