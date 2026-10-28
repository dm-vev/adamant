// Package nethernet implements NetherNet, the transport of Minecraft:
// Bedrock Edition since 1.26.50: WebRTC data channels negotiated over HTTP
// signaling. Every connection of a Listener shares the same UDP sockets
// and no goroutine of its own; a Dialer opens client connections for bots
// and proxies.
package nethernet
