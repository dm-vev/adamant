// Package bedrockadapter is the adapter of the server for clients of
// Bedrock Edition. Its Config, made of the keys of config.toml it owns by
// UserConfig.Config, gives the NetherNet listener of the server through
// Config.Listener: a server.Listener whose clients join over NetherNet, LAN,
// Xbox Live or proxies, answered by the session of package session.
//
// It is named bedrockadapter, not bedrock, so that it reads apart from
// net/bedrock, the protocol library it is built on.
package bedrockadapter
