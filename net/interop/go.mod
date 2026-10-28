module github.com/dm-vev/adamant/net/interop

go 1.26.0

require (
	github.com/dm-vev/adamant/net v0.0.0
	github.com/pion/dtls/v3 v3.1.9
	github.com/pion/logging v0.2.4
	github.com/pion/sctp v1.8.40
)

require (
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/transport/v3 v3.0.8 // indirect
	github.com/pion/transport/v5 v5.0.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
)

replace github.com/dm-vev/adamant/net => ../
