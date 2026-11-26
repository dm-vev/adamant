package netconn

import "github.com/dm-vev/adamant/protocol/bedrock/internal/versions"

// oldProtocol returns protocol 2193, the oldest the adapter speaks.
func oldProtocol() *versions.Protocol {
	p, _ := versions.ByNumber(2193)
	return p
}
