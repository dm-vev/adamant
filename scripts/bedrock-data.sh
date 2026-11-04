#!/bin/bash
# Takes the vanilla data of a protocol from the Bedrock Dedicated Server of
# that protocol, as protocol/bedrock/internal/bedrockdata/vNNNN/ holds it.
#
#   scripts/bedrock-data.sh BDS_DIR PROTOCOL OUT_DIR [PORT]
#
# BDS_DIR is an extracted BDS, such as /home/agent/bds/1.26.61.1, which
# scripts/bds-run.sh runs a copy of on PORT (19232 if unset); then
# cmd/vanilla-probe joins it as a client of PROTOCOL and writes into OUT_DIR:
#
#   items.nbt      the ItemRegistry BDS sends
#   blocks.nbt     the block definitions of its StartGame
#   biomes.bin     the body of its BiomeDefinitionList (to a client that
#                  generates chunks itself, which BDS sends the generation
#                  data of biomes to)
#   entities.json  the identifiers of its AvailableActorIdentifiers
#   creative.json  its creative inventory, items by name
#   crafting.json  its recipes, items by name
#   snapshot.json  what the probe prints: the version, the items, the biomes
#
# BDS is stopped once the probe is done. OUT_DIR must not hold any of the
# files. The script prints the SHA-256 of each file it wrote, for the
# PROVENANCE file of the data.
set -eu

if [ $# -lt 3 ]; then
	echo "usage: $0 BDS_DIR PROTOCOL OUT_DIR [PORT]" >&2
	exit 2
fi
bds=$(cd "$1" && pwd)
protocol=$2
out=$3
port=${4:-19232}
repo=$(cd "$(dirname "$0")/.." && pwd)

if [ ! -x "$bds/bedrock_server" ]; then
	echo "$bds holds no bedrock_server" >&2
	exit 1
fi
mkdir -p "$out"
for f in items.nbt blocks.nbt biomes.bin entities.json creative.json crafting.json snapshot.json; do
	if [ -e "$out/$f" ]; then
		echo "$out/$f exists" >&2
		exit 1
	fi
done

mkdir -p "${BDS_RUN:-/home/agent/bds-run}"
probe="${BDS_RUN:-/home/agent/bds-run}/vanilla-probe"
(cd "$repo" && nice -n 10 go build -o "$probe" ./cmd/vanilla-probe)
"$repo/scripts/bds-run.sh" "$bds" "$port" sh -c '
	exec nice -n 10 "$0" -address "$BDS_ADDR" -protocol "$1" \
		-items-nbt "$2/items.nbt" -blocks-nbt "$2/blocks.nbt" -biomes-packet "$2/biomes.bin" \
		-entities "$2/entities.json" -creative "$2/creative.json" -crafting "$2/crafting.json" >"$2/snapshot.json"
' "$probe" "$protocol" "$out"
(cd "$out" && sha256sum items.nbt blocks.nbt biomes.bin entities.json creative.json crafting.json snapshot.json)
