package netconn

import "github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"

// oldProtocol returns protocol 2193, the oldest the adapter speaks.
func oldProtocol() *versions.Protocol {
	p, _ := versions.ByNumber(2193)
	return p
}
