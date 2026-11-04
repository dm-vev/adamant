#!/bin/bash
# Takes the vanilla data of a protocol from the Bedrock Dedicated Server of
# that protocol, as protocol/bedrock/internal/bedrockdata/vNNNN/ holds it.
#
#   scripts/bedrock-data.sh BDS_DIR PROTOCOL OUT_DIR [PORT]
#
# BDS_DIR is an extracted BDS, such as /home/agent/bds/1.26.61.1, which is
# never changed: it is copied to /home/agent/bds-run/<its name>/ (or
# $BDS_RUN), where BDS writes its world and logs. The copy is set up as an
# offline NetherNet server on PORT (19232 if unset) and started; then
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
run=${BDS_RUN:-/home/agent/bds-run}/$(basename "$bds")
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

# A fresh copy each run: BDS changes its folder as it runs.
rm -rf "$run"
mkdir -p "$run"
cp -a "$bds/." "$run/"
props="$run/server.properties"
set_property() {
	if grep -q "^$1=" "$props"; then
		sed -i "s|^$1=.*|$1=$2|" "$props"
	else
		echo "$1=$2" >>"$props"
	fi
}
set_property transport nethernet
set_property server-port "$port"
set_property online-mode false
set_property allow-list false
set_property enable-lan-visibility false
set_property level-name probe
set_property allow-cheats true

(cd "$run" && LD_LIBRARY_PATH=. exec nice -n 10 ./bedrock_server >"$run/server.log" 2>&1 </dev/null) &
server=$!
stop() {
	kill "$server" 2>/dev/null || true
	wait "$server" 2>/dev/null || true
}
trap stop EXIT INT TERM

# BDS takes a while to make its world.
i=0
until grep -q "Server started" "$run/server.log" 2>/dev/null; do
	i=$((i + 1))
	if [ $i -gt 120 ] || ! kill -0 "$server" 2>/dev/null; then
		echo "BDS did not start; see $run/server.log" >&2
		exit 1
	fi
	sleep 1
done

# The signaling of NetherNet may answer a moment after BDS says it started.
cd "$repo"
nice -n 10 go build -o "$run/vanilla-probe" ./cmd/vanilla-probe
i=0
until (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; do
	i=$((i + 1))
	if [ $i -gt 30 ]; then
		echo "BDS does not answer on port $port; see $run/server.log" >&2
		exit 1
	fi
	sleep 1
done
nice -n 10 "$run/vanilla-probe" -address "http://127.0.0.1:$port" -protocol "$protocol" \
	-items-nbt "$out/items.nbt" -blocks-nbt "$out/blocks.nbt" -biomes-packet "$out/biomes.bin" \
	-entities "$out/entities.json" -creative "$out/creative.json" -crafting "$out/crafting.json" \
	>"$out/snapshot.json"
grep -m1 -o 'Version: [0-9.]*' "$run/server.log" || true
(cd "$out" && sha256sum items.nbt blocks.nbt biomes.bin entities.json creative.json crafting.json snapshot.json)
