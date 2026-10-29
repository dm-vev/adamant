//go:build linux

package nethernet

// sysSendmmsg is the number of sendmmsg, which package syscall leaves out
// on amd64.
const sysSendmmsg = 307
