#!/usr/bin/env bash
# loadtest_compare.sh builds the server of each commit passed and runs the
# same load-test scenarios against each, then the microbenchmarks that exist
# at every commit, and writes summary.json. The first commit is the base the
# others are compared with.
#
#   scripts/loadtest_compare.sh 281cf3c8 813fe6dd
#
# Each commit is exported with git archive (no worktrees) and built with the
# go of PATH. Runs alternate the commits within every repetition, so that a
# change of the load on the machine falls on all of them alike. The load
# test itself (cmd/loadtest) is built once, from the checkout the script is
# run from.
#
# Environment (defaults in brackets):
#   RESULTS      results directory [/home/agent/perf-results]
#   SCENARIOS    world:bots pairs [flat:1 flat:10 flat:30 vanilla:10]
#   REPS         repetitions of each scenario [3]
#   DURATION     measured window of a run [60s]
#   PORT         port of the server [19700]
#   SERVER_CPUS  CPUs the server is pinned to, empty for none [0-3]
#   BOT_CPUS     CPUs the bots are pinned to, empty for none [4-7]
#   LOADTEST     extra flags of loadtest run []
#   BENCH        1 to run the microbenchmarks, 0 to skip them [1]
#   BENCH_COUNT  rounds of the microbenchmarks [10]
#   LOADTESTS    1 to run the load tests, 0 to skip them [1]
#   RESUME       1 to keep the results of runs already done [1]
#   KEEP_WORK    1 to keep the exported trees and run folders [0]
set -euo pipefail
export LC_ALL=C

RESULTS=${RESULTS:-/home/agent/perf-results}
SCENARIOS=${SCENARIOS:-"flat:1 flat:10 flat:30 vanilla:10"}
REPS=${REPS:-3}
DURATION=${DURATION:-60s}
PORT=${PORT:-19700}
SERVER_CPUS=${SERVER_CPUS-0-3}
BOT_CPUS=${BOT_CPUS-4-7}
LOADTEST=${LOADTEST:-}
BENCH=${BENCH:-1}
BENCH_COUNT=${BENCH_COUNT:-10}
LOADTESTS=${LOADTESTS:-1}
RESUME=${RESUME:-1}
KEEP_WORK=${KEEP_WORK:-0}

repo=$(git rev-parse --show-toplevel)
if [ $# -lt 1 ]; then
	echo "usage: $0 base-commit [commit...]" >&2
	exit 2
fi
commits=()
for c in "$@"; do
	commits+=("$(git -C "$repo" rev-parse --short=8 "$c^{commit}")")
done
base=${commits[0]}

mkdir -p "$RESULTS/bin" "$RESULTS/worlds"
work=$(mktemp -d "${TMPDIR:-/tmp}/loadtest-compare.XXXXXX")
cleanup() {
	if [ -n "${server_pid:-}" ] && kill -0 "$server_pid" 2>/dev/null; then
		kill -9 "$server_pid" 2>/dev/null || true
	fi
	if [ "$KEEP_WORK" != 1 ]; then
		rm -rf "$work"
	fi
}
trap cleanup EXIT

log() { echo "[$(date +%H:%M:%S)] $*" >&2; }

pin() {
	# pin CPUS command...: runs the command on the CPUS, or anywhere if empty.
	local cpus=$1
	shift
	if [ -n "$cpus" ]; then
		exec taskset -c "$cpus" "$@"
	fi
	exec "$@"
}

log "building loadtest from $repo"
(cd "$repo" && go build -trimpath -o "$RESULTS/bin/loadtest" ./cmd/loadtest)
loadtest=$RESULTS/bin/loadtest

for c in "${commits[@]}"; do
	src=$work/src-$c
	mkdir -p "$src" "$RESULTS/$c/bin"
	git -C "$repo" archive "$c" | tar -x -C "$src"
	git -C "$repo" rev-parse "$c^{commit}" >"$RESULTS/$c/commit.txt"
	go version >"$RESULTS/$c/go-version.txt"
	if [ "$LOADTESTS" = 1 ]; then
		log "building the server of $c"
		(cd "$src" && go build -trimpath -o "$RESULTS/$c/bin/adamant" .)
	fi
done

# run_one commit world bots rep runs one scenario against one commit.
run_one() {
	local c=$1 world=$2 bots=$3 rep=$4
	local dir=$RESULTS/$c/loadtest/$world-$bots
	local out=$dir/rep$rep.json
	if [ "$RESUME" = 1 ] && [ -s "$out" ]; then
		log "keeping $out"
		return
	fi
	mkdir -p "$dir"
	local run=$work/run
	rm -rf "$run"
	mkdir -p "$run"
	cp -a "$RESULTS/worlds/$world" "$run/world"
	cat >"$run/config.toml" <<EOF
[Network]
  Address = "127.0.0.1:$PORT"
[Server]
  Name = "loadtest"
  AuthEnabled = false
[World]
  Folder = "world"
  Seed = 1
  SaveData = true
  DisableNether = true
  DisableEnd = true
[Players]
  MaximumChunkRadius = 8
  SaveData = false
EOF
	log "run $c $world-$bots rep $rep"
	local launched
	launched=$(date +%s%N)
	(cd "$run" && pin "$SERVER_CPUS" "$RESULTS/$c/bin/adamant" -headless >server.log 2>&1) &
	server_pid=$!
	local status=0
	# shellcheck disable=SC2086
	(pin "$BOT_CPUS" "$loadtest" run -addr "http://127.0.0.1:$PORT" -bots "$bots" -duration "$DURATION" \
		-pid "$server_pid" -server-log "$run/server.log" -launched "$launched" \
		-labels "commit=$c,world=$world,bots=$bots,rep=$rep,scenario=$world-$bots" \
		-out "$out.tmp" $LOADTEST) >"$dir/rep$rep.txt" 2>&1 || status=$?
	kill -INT "$server_pid" 2>/dev/null || true
	for _ in $(seq 1 600); do
		kill -0 "$server_pid" 2>/dev/null || break
		sleep 0.1
	done
	if kill -0 "$server_pid" 2>/dev/null; then
		log "server of $c did not stop in 60s: killed"
		kill -9 "$server_pid" 2>/dev/null || true
	fi
	wait "$server_pid" 2>/dev/null || true
	server_pid=
	gzip -c "$run/server.log" >"$dir/rep$rep.server.log.gz"
	cat "$dir/rep$rep.txt" >&2
	if [ $status -ne 0 ]; then
		log "run $c $world-$bots rep $rep failed with status $status"
		rm -f "$out.tmp"
		return
	fi
	mv "$out.tmp" "$out"
}

if [ "$LOADTESTS" = 1 ]; then
	for scenario in $SCENARIOS; do
		world=${scenario%%:*}
		if [ ! -d "$RESULTS/worlds/$world" ]; then
			log "generating the $world world"
			"$loadtest" pregen -world "$RESULTS/worlds/$world" -kind "$world" -seed 1 -radius 24
		fi
	done
	for rep in $(seq 1 "$REPS"); do
		for scenario in $SCENARIOS; do
			world=${scenario%%:*}
			bots=${scenario##*:}
			# Odd repetitions run the commits in the order given, even ones
			# in the reverse order.
			order=("${commits[@]}")
			if [ $((rep % 2)) -eq 0 ]; then
				order=()
				for ((i = ${#commits[@]} - 1; i >= 0; i--)); do order+=("${commits[$i]}"); done
			fi
			for c in "${order[@]}"; do
				run_one "$c" "$world" "$bots" "$rep"
			done
		done
	done
fi

# benchmarks src lists the benchmarks of an exported tree, as
# "module-dir package-dir name" lines. Modules other than the main one and
# net/ are left out.
benchmarks() {
	local src=$1
	(cd "$src" && grep -rEo --include='*_test.go' '^func Benchmark[A-Za-z0-9_]*' . |
		sed -E 's|^\./||; s|/[^/]*_test\.go:func | |' |
		grep -Ev '^(fixes|scripts|net/interop)/' |
		awk '{ if ($1 ~ /^net(\/|$)/) { m = "net"; p = substr($1, 5); if (p == "") p = "." } else { m = "."; p = $1 } print m, p, $2 }' |
		sort -u)
}

if [ "$BENCH" = 1 ]; then
	mkdir -p "$RESULTS/bench"
	# The benchmarks every commit has.
	common=$work/bench-common
	benchmarks "$work/src-$base" >"$common"
	for c in "${commits[@]:1}"; do
		benchmarks "$work/src-$c" | comm -12 "$common" - >"$common.next"
		mv "$common.next" "$common"
	done
	cp "$common" "$RESULTS/bench/common.txt"
	for c in "${commits[@]}"; do
		benchmarks "$work/src-$c" | comm -13 "$common" - >"$RESULTS/$c/bench-only-here.txt"
	done
	log "$(wc -l <"$common") benchmarks are at every commit"

	# One test binary for each package and commit, built once.
	pkgs=$(awk '{ print $1 " " $2 }' "$common" | sort -u)
	for c in "${commits[@]}"; do
		mkdir -p "$work/bench-$c" "$RESULTS/$c/bench"
		: >"$RESULTS/$c/bench/micro.txt"
		while read -r m p; do
			name=$(echo "$m/$p" | tr '/.' '__')
			(cd "$work/src-$c/$m" && go test -c -o "$work/bench-$c/$name.test" "./$p") ||
				log "benchmarks of $m/$p do not build at $c"
		done <<<"$pkgs"
	done
	# Rounds alternate the commits, one run of each benchmark a round.
	for round in $(seq 1 "$BENCH_COUNT"); do
		log "benchmark round $round of $BENCH_COUNT"
		for c in "${commits[@]}"; do
			while read -r m p; do
				name=$(echo "$m/$p" | tr '/.' '__')
				bin=$work/bench-$c/$name.test
				[ -x "$bin" ] || continue
				names=$(awk -v m="$m" -v p="$p" '$1 == m && $2 == p { print $3 }' "$common" | paste -sd'|')
				(cd "$work/src-$c/$m/$p" && pin "$SERVER_CPUS" "$bin" -test.run '^$' -test.bench "^($names)\$" \
					-test.count 1 -test.benchmem -test.timeout 60m) >>"$RESULTS/$c/bench/micro.txt" 2>&1 ||
					log "benchmarks of $m/$p failed at $c"
			done <<<"$pkgs"
		done
	done
	benchstat=${BENCHSTAT:-$(command -v benchstat || echo /home/agent/go/bin/benchstat)}
	for c in "${commits[@]:1}"; do
		"$benchstat" "$base=$RESULTS/$base/bench/micro.txt" "$c=$RESULTS/$c/bench/micro.txt" >"$RESULTS/bench/$base-vs-$c.txt" || true
		"$benchstat" -format csv "$base=$RESULTS/$base/bench/micro.txt" "$c=$RESULTS/$c/bench/micro.txt" >"$RESULTS/bench/$base-vs-$c.csv" || true
	done

	# The text paths, old against new, at the checkout the script runs
	# from.
	log "text benchmarks of $(git -C "$repo" rev-parse --short=8 HEAD)"
	(cd "$repo" && go test -c -o "$work/textbench.test" ./cmd/loadtest)
	: >"$RESULTS/bench/text.txt"
	for _ in $(seq 1 "$BENCH_COUNT"); do
		(cd "$repo/cmd/loadtest" && pin "$SERVER_CPUS" "$work/textbench.test" -test.run '^$' -test.bench . \
			-test.count 1 -test.benchmem) >>"$RESULTS/bench/text.txt"
	done
	git -C "$repo" rev-parse HEAD >"$RESULTS/bench/text-commit.txt"
fi

"$loadtest" summarize -results "$RESULTS" -base "$base" -commits "$(
	IFS=,
	echo "${commits[*]}"
)" -out "$RESULTS/summary.json"
log "wrote $RESULTS/summary.json"
