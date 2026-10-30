#!/usr/bin/env python3
"""Capture native redstone timing using an isolated stable-API behavior pack.

The script only drives commands and reads block permutations. BDS performs all
redstone simulation. Samples are taken before this tick's scripted changes, so
the trace records exactly which phase is observed rather than guessing timing
from console round trips or wall-clock sleeps.
"""

import argparse
import hashlib
import json
from pathlib import Path

from redstone_bds_probe import Reference


SCRIPT = r'''
import { world, system } from "@minecraft/server";
const emit = value => console.warn("ADAMANT_TIMING " + JSON.stringify(value));
const cases = [];
for (let delay = 0; delay < 4; delay++) {
    for (const width of [1, 2, 3, 4, 8, 16]) {
        for (const phase of [0, 1]) cases.push({delay, width, phase});
    }
}
let index = -1, start = 0, on = 0, samples = [], changes = [], dimension, loadStarted;
function cmd(command) { dimension.runCommand(command); }
function read(x) {
    const b = dimension.getBlock({x, y: 65, z: 0});
    if (!b) throw new Error("sampled chunk not loaded");
    return {id: b.typeId, states: b.permutation.getAllStates()};
}
function setup() {
    const c = cases[index];
    cmd("fill -3 64 -2 6 68 2 air");
    cmd("fill -3 64 -2 6 64 2 stone");
    cmd(`setblock 0 65 0 unpowered_repeater ["minecraft:cardinal_direction"="west","repeater_delay"=${c.delay}]`);
    cmd('setblock 1 65 0 unpowered_repeater ["minecraft:cardinal_direction"="west"]');
    cmd("setblock 2 65 0 redstone_lamp");
    samples = []; changes = []; start = system.currentTick;
    // Control ABSOLUTE tick parity, independent of preceding scenario lengths.
    on = 24 + ((c.phase - ((start + 24) & 1) + 2) % 2);
}
const handle = system.runInterval(() => {
    try {
        if (!dimension) {
            dimension = world.getDimension("overworld");
            cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");
            cmd("gamerule randomtickspeed 0");
            loadStarted = system.currentTick;
            return;
        }
        if (index < 0) {
            if (!dimension.getBlock({x: -3, y: 65, z: -2}) || !dimension.getBlock({x: 6, y: 65, z: 2})) {
                if (system.currentTick - loadStarted > 200) throw new Error("ticking area failed to load");
                return;
            }
            emit({ready: true}); index = 0; setup(); return;
        }
        const c = cases[index], t = system.currentTick - start;
        samples.push({t, tick: system.currentTick, source: read(-1), first: read(0), second: read(1), lamp: read(2)});
        if (t === on || t === on + c.width) {
            const powered = t === on;
            cmd("setblock -1 65 0 " + (powered ? "redstone_block" : "air"));
            changes.push({t, tick: system.currentTick, powered});
        }
        if (t >= on + c.width + 28) {
            emit({scenario: c, samples, changes});
            index++;
            if (index === cases.length) {
                emit({complete: true, count: cases.length}); system.clearRun(handle);
            } else setup();
        }
    } catch (error) {
        emit({error: String(error), stack: error.stack}); system.clearRun(handle);
    }
}, 1);
'''


def arguments():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--assets", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--port", type=int, default=30861)
    return parser.parse_args()


def capture(args, script, expected_count, structures=None, entity_definitions=None):
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port, script=script, structures=structures,
                          local_client=getattr(args, "offline_reference", False), entity_definitions=entity_definitions)
    with args.binary.open("rb") as stream:
        binary_hash = hashlib.file_digest(stream, "sha256").hexdigest()
    output = {"reference": "unmodified Windows BDS, stable Script API readback", "binary_sha256": binary_hash,
              "complete": False, "sample_phase": "before scripted source changes in system.runInterval", "scenarios": []}
    if getattr(args, "offline_reference", False):
        output["offline_reference"] = True
    try:
        while True:
            lines = reference.until(lambda line: "ADAMANT_TIMING " in line, timeout=45)
            line = lines[-1].split("ADAMANT_TIMING ", 1)[1].strip()
            record = json.loads(line)
            if "error" in record:
                raise RuntimeError(record)
            if "scenario" in record:
                output["scenarios"].append(record)
                print(json.dumps({"scenario": record["scenario"], "count": len(output["scenarios"])}), flush=True)
            if record.get("complete"):
                if record["count"] != expected_count or len(output["scenarios"]) != expected_count:
                    raise RuntimeError("incomplete timing capture")
                for case in output["scenarios"]:
                    if case["changes"][0]["tick"] % 2 != case["scenario"]["phase"]:
                        raise RuntimeError("timing capture did not control absolute tick parity")
                output["complete"] = True
                break
    finally:
        reference.stop()
        output["log_directory"] = str(reference.root)
        output["build_log"] = [line for line in reference.log if "Version:" in line or "Build ID:" in line]
        args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
        print(json.dumps({"output": str(args.output), "complete": output["complete"], "logs": str(reference.root)}))


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 48)
