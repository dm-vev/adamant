// Package client holds the contract between the game and the client of a
// player, whichever protocol it speaks. Controllable is what a client drives
// its player through, and Diagnostics what a client tells of how it runs. A
// protocol adapter, such as the Bedrock Edition session, drives a
// Controllable; the game never learns the protocol behind it.
//
// The package imports no protocol: neither net/, nor server/session,
// server/netconn or protocol/.
package client
