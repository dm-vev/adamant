// Package javaadapter is the adapter of the server for clients of Java
// Edition. So far it shows the server in their server lists, with the
// status of the server, and tells a client that joins that the server is
// one of Bedrock Edition: Config.Listener is a server.Listener that never
// yields a Join.
//
// It is named javaadapter, not java, so that it reads apart from net/java,
// the protocol library it is built on.
package javaadapter
