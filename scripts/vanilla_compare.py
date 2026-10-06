#!/usr/bin/env python3
"""Run fresh BDS and Adamant worlds in an isolated Linux network and compare snapshots."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import threading
import time


ROOT = Path(__file__).resolve().parents[1]
ADDRESS = "192.0.2.1"


def run(command, **kwargs):
    return subprocess.run(command, check=True, **kwargs)


def sha256(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


class Server:
    def __init__(self, command, directory, log_path, ready_text, timeout):
        self.log = log_path.open("w", encoding="utf-8")
        self.ready = threading.Event()
        self.version = None
        self.build = None
        self.process = subprocess.Popen(
            command, cwd=directory, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT, text=True, encoding="utf-8", errors="replace",
            env={**os.environ, "LD_LIBRARY_PATH": str(directory)},
        )
        self.thread = threading.Thread(target=self.consume, args=(ready_text,), daemon=True)
        self.thread.start()
        deadline = time.monotonic() + timeout
        while not self.ready.wait(0.1):
            if self.process.poll() is not None or time.monotonic() >= deadline:
                self.stop()
                raise RuntimeError(f"server did not become ready; inspect {log_path}")

    def consume(self, ready_text):
        for line in self.process.stdout:
            self.log.write(line)
            self.log.flush()
            if match := re.search(r"\bVersion: ([0-9.]+)", line):
                self.version = match[1]
            if match := re.search(r"\bBuild ID: ([0-9]+)", line):
                self.build = int(match[1])
            if ready_text in line:
                self.ready.set()

    def stop(self, bds=False):
        if self.process.poll() is None:
            if bds:
                try:
                    self.process.stdin.write("stop\n")
                    self.process.stdin.flush()
                except BrokenPipeError:
                    pass
            else:
                self.process.send_signal(signal.SIGTERM)
            try:
                self.process.wait(timeout=30)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()
        self.thread.join(timeout=5)
        self.process.stdin.close()
        self.process.stdout.close()
        self.log.close()


def run_isolated(output, timeout, block_catalog, reference_only):
    # Configure ICE before starting BDS. A loopback-only BDS has no usable candidates.
    run(["ip", "link", "set", "lo", "up"])
    run(["ip", "link", "add", "parity0", "type", "dummy"])
    run(["ip", "addr", "add", f"{ADDRESS}/24", "dev", "parity0"])
    run(["ip", "link", "set", "parity0", "up"])
    reference = json.loads((output / "reference-manifest.json").read_text())
    bds = candidate = None
    try:
        ready = 'ADAMANT_BLOCK_CATALOG {"event":"complete"' if block_catalog else "Server started."
        bds = Server([str(output / "bds" / "bedrock_server")], output / "bds",
                     output / "bds.log", ready, timeout)
        if bds.version != reference["version"] or bds.build != reference["build_id"]:
            raise RuntimeError(f"reference version/build mismatch: {bds.version}/{bds.build}")
        targets = [("reference", 19132)]
        if not reference_only:
            candidate = Server([str(output / "bin" / "adamant"), "-headless"],
                               output / "candidate", output / "candidate.log",
                               "Dragonfly server started.", timeout)
            targets.append(("candidate", 19134))
        for name, port in targets:
            with (output / f"{name}.json").open("w") as snapshot, (output / f"{name}-probe.log").open("w") as log:
                command = [str(output / "bin" / "vanilla-probe"),
                           "-address", f"http://{ADDRESS}:{port}", "-timeout", f"{timeout}s",
                           "-items-nbt", str(output / f"{name}-items.nbt"),
                           "-biomes-packet", str(output / f"{name}-biomes.bin")]
                if block_catalog and name == "reference":
                    command += ["-wait-message", "ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"]
                run(command, stdout=snapshot, stderr=log, timeout=timeout + 5)
        if reference_only:
            return 0
        with (output / "comparison.json").open("w") as report:
            result = subprocess.run([str(output / "bin" / "vanilla-compare"),
                                     "-reference", str(output / "reference.json"),
                                     "-candidate", str(output / "candidate.json")], stdout=report)
        return result.returncode
    finally:
        if candidate is not None:
            candidate.stop()
        if bds is not None:
            bds.stop(bds=True)


def prepare(bds_directory, output, seed, block_catalog, reference_only):
    if not (bds_directory / "bedrock_server").is_file():
        raise ValueError("--bds-dir must contain an extracted Linux bedrock_server binary")
    if output.is_relative_to(bds_directory):
        raise ValueError("--output must be outside --bds-dir")
    if not -(1 << 63) <= seed < (1 << 63):
        raise ValueError("seed must fit a signed 64-bit integer")
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    (output / "bin").mkdir()
    (output / "bds").mkdir()
    # Copy only distribution assets; never reuse worlds, players, allowlists or caches.
    for name in ("bedrock_server", "behavior_packs", "resource_packs", "definitions",
                 "config", "data", "profanity_filter.wlist", "packetlimitconfig.json"):
        source = bds_directory / name
        if source.is_dir():
            shutil.copytree(source, output / "bds" / name)
        elif source.is_file():
            shutil.copy2(source, output / "bds" / name)
    for library in bds_directory.glob("*.so*"):
        if library.is_file():
            shutil.copy2(library, output / "bds" / library.name)
    shutil.copy2(ROOT / "vanilla-reference.json", output / "reference-manifest.json")
    (output / "bds" / "server.properties").write_text(
        "server-name=Adamant Vanilla Reference\n"
        "gamemode=survival\ndifficulty=normal\nonline-mode=false\nallow-list=false\n"
        "transport=nethernet\nserver-port=19132\nserver-portv6=19133\n"
        "enable-lan-visibility=false\nview-distance=5\nmax-players=2\n"
        "client-side-chunk-generation-enabled=true\n"
        f"level-name=reference\nlevel-seed={seed}\nemit-server-telemetry=false\n"
        "content-log-console-output-enabled=true\n"
    )
    (output / "bds" / "allowlist.json").write_text("[]\n")
    (output / "bds" / "permissions.json").write_text("[]\n")
    if block_catalog:
        pack = ROOT / "scripts" / "vanilla-block-catalog"
        shutil.copytree(pack, output / "bds" / "behavior_packs" / "adamant_block_catalog")
        header = json.loads((pack / "manifest.json").read_text())["header"]
        world = output / "bds" / "worlds" / "reference"
        world.mkdir(parents=True)
        (world / "world_behavior_packs.json").write_text(
            json.dumps([{"pack_id": header["uuid"], "version": header["version"]}]) + "\n"
        )
    programs = [("vanilla-probe", "./cmd/vanilla-probe")]
    if not reference_only:
        (output / "candidate").mkdir()
        (output / "candidate" / "config.toml").write_text(
            f'[Network]\nAddress="{ADDRESS}:19134"\nTransport="nethernet"\nEnableQuery=false\n'
            '[Server]\nName="Adamant Vanilla Candidate"\nAuthEnabled=false\n'
            f'[World]\nSaveData=true\nSeed={seed}\nGeneratorWorkers=2\n'
            '[Players]\nSaveData=true\nMaximumChunkRadius=2\n'
            '[Resources]\nAutoBuildPack=false\n'
        )
        programs += [("adamant", "."), ("vanilla-compare", "./cmd/vanilla-compare")]
    if block_catalog:
        programs += [("vanilla-palettes", "./cmd/vanilla-palettes"),
                     ("vanilla-block-catalog", "./cmd/vanilla-block-catalog")]
    for name, package in programs:
        run(["go", "build", "-o", str(output / "bin" / name), package], cwd=ROOT)
    provenance = {
        "scope": "initial registry and world settings; not gameplay parity",
        "seed": seed,
        "block_catalog": block_catalog,
        "reference_only": reference_only,
        "bds_binary_sha256": sha256(output / "bds" / "bedrock_server"),
    }
    provenance["binaries"] = {name: sha256(output / "bin" / name) for name, _ in programs}
    (output / "run.json").write_text(json.dumps(provenance, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bds-dir", type=Path, help="extracted pinned Linux BDS distribution")
    parser.add_argument("--output", type=Path, required=True, help="new directory for worlds, logs and reports")
    parser.add_argument("--seed", type=int, default=0)
    parser.add_argument("--timeout", type=int, default=60, help="each startup/probe deadline in seconds")
    parser.add_argument("--block-catalog", action="store_true",
                        help="also export BDS block permutations through temporary saved structures")
    parser.add_argument("--reference-only", action="store_true",
                        help="capture BDS reference data without building or starting Adamant")
    parser.add_argument("--isolated", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    if sys.platform != "linux":
        parser.error("this runner requires Linux user/network namespaces")
    if args.timeout <= 0:
        parser.error("--timeout must be positive")
    output = args.output.resolve()
    if args.isolated:
        parent_namespace = os.environ.get("ADAMANT_REFERENCE_PARENT_NETNS")
        if not parent_namespace or os.readlink("/proc/self/ns/net") == parent_namespace:
            raise RuntimeError("internal runner requires a fresh isolated network namespace")
        return run_isolated(output, args.timeout, args.block_catalog, args.reference_only)
    if args.bds_dir is None:
        parser.error("--bds-dir is required")
    for program in ("unshare", "ip", "go"):
        if shutil.which(program) is None:
            parser.error(f"required executable not found: {program}")
    # Check namespace support before copying/building. Never fall back to host networking.
    run(["unshare", "--user", "--map-root-user", "--net", "true"])
    prepare(args.bds_dir.resolve(), output, args.seed, args.block_catalog, args.reference_only)
    command = ["unshare", "--user", "--map-root-user", "--net",
               sys.executable, str(Path(__file__).resolve()), "--isolated",
               "--output", str(output), "--timeout", str(args.timeout)]
    if args.block_catalog:
        command.append("--block-catalog")
    if args.reference_only:
        command.append("--reference-only")
    result = subprocess.run(
        command, env={**os.environ, "ADAMANT_REFERENCE_PARENT_NETNS": os.readlink("/proc/self/ns/net")}
    )
    if args.block_catalog and result.returncode in (0, 1):
        with (output / "reference-palettes.json").open("w") as report:
            run([str(output / "bin" / "vanilla-palettes"),
                 "-db", str(output / "bds" / "worlds" / "reference" / "db"),
                 "-nbt", str(output / "reference-world-states.nbt")], stdout=report)
        versions = json.loads((output / "reference-palettes.json").read_text())["palette_version_counts"]
        if len(versions) != 1:
            raise RuntimeError(f"expected one palette version in a fresh BDS world, found {versions}")
        with (output / "reference-block-catalog.json").open("w") as report:
            run([str(output / "bin" / "vanilla-block-catalog"), "-log", str(output / "bds.log"),
                 "-db", str(output / "bds" / "worlds" / "reference" / "db"),
                 "-world-states", str(output / "reference-world-states.nbt"),
                 "-version", next(iter(versions)), "-output", str(output / "reference-block-states.nbt")],
                stdout=report)
    print(f"Comparison artifacts: {output}")
    return result.returncode


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print(f"vanilla_compare: {error}", file=sys.stderr)
        sys.exit(2)
