#!/bin/bash
# Runs a command against a copy of a pristine Bedrock Dedicated Server.
#
#   scripts/bds-run.sh BDS_DIR PORT COMMAND [ARG...]
#
# BDS_DIR is an extracted BDS, such as /home/agent/bds/1.26.61.1, which is
# never changed: it is copied to /home/agent/bds-run/<its name>/ (or
# $BDS_RUN), where BDS writes its world and logs. The copy is set up as an
# offline NetherNet server on PORT, without an allow list, with cheats on
# and its players operators, and started. Once it answers, COMMAND runs with
# BDS_ADDR set to its signaling URL, http://127.0.0.1:PORT; then BDS is
# stopped, and the script exits as COMMAND did.
set -eu

if [ $# -lt 3 ]; then
	echo "usage: $0 BDS_DIR PORT COMMAND [ARG...]" >&2
	exit 2
fi
bds=$(cd "$1" && pwd)
port=$2
shift 2
run=${BDS_RUN:-/home/agent/bds-run}/$(basename "$bds")

if [ ! -x "$bds/bedrock_server" ]; then
	echo "$bds holds no bedrock_server" >&2
	exit 1
fi

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
set_property default-player-permission-level operator

(cd "$run" && LD_LIBRARY_PATH=. exec nice -n 10 ./bedrock_server >"$run/server.log" 2>&1 </dev/null) &
server=$!
stop() {
	kill "$server" 2>/dev/null || true
	wait "$server" 2>/dev/null || true
}
trap stop EXIT INT TERM

# BDS takes a while to make its world, and its signaling may answer a
# moment after it says it started.
i=0
until grep -q "Server started" "$run/server.log" 2>/dev/null && (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; do
	i=$((i + 1))
	if [ $i -gt 150 ] || ! kill -0 "$server" 2>/dev/null; then
		echo "BDS did not start; see $run/server.log" >&2
		exit 1
	fi
	sleep 1
done
grep -m1 -o 'Version: [0-9.]*' "$run/server.log" >&2 || true

BDS_ADDR="http://127.0.0.1:$port" "$@"
