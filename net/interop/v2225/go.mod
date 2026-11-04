module github.com/dm-vev/adamant/net/interop/v2225

go 1.27.0

toolchain go1.27.3

require (
	github.com/dm-vev/adamant/net v0.0.0
	github.com/go-gl/mathgl v1.2.0
	github.com/sandertv/gophertunnel v1.63.1-0.20261030102901-0e179fc4bb08
	golang.org/x/tools v0.41.0
)

require (
	github.com/dm-vev/adamant/net/interop v0.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.18.4 // indirect
	golang.org/x/exp v0.0.0-20250103183323-7d7fa50e5329 // indirect
	golang.org/x/mod v0.32.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
)

tool (
	github.com/dm-vev/adamant/net/interop/cmd/constimport
	github.com/dm-vev/adamant/net/interop/cmd/protoimport
)

replace github.com/dm-vev/adamant/net => ../../

replace github.com/dm-vev/adamant/net/interop => ../

replace github.com/df-mc/dragonfly => ../../..
