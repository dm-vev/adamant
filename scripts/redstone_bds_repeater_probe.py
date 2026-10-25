#!/usr/bin/env python3
"""Capture repeater side-lock filtering in a fresh, isolated native BDS world."""

import argparse
import hashlib
import json
from pathlib import Path
import time

from redstone_bds_probe import Reference


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--assets", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--port", type=int, default=30861)
    args = parser.parse_args()
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port)
    with args.binary.open("rb") as stream:
        binary_hash = hashlib.file_digest(stream, "sha256").hexdigest()
    output = {"reference": "unmodified Windows BDS", "binary_sha256": binary_hash, "complete": False, "scenarios": []}
    try:
        reference.command("tickingarea add -16 0 -16 16 100 16 redstone_audit true")
        reference.command("gamerule randomtickspeed 0")
        cases = {
            "redstone_block": ["setblock 0 65 1 redstone_block"],
            "wire": ["setblock 0 65 1 redstone_wire", "setblock 0 65 2 redstone_block"],
            "torch": ['setblock 0 65 1 redstone_torch ["torch_facing_direction"="top"]'],
            "repeater": ['setblock 0 65 1 unpowered_repeater ["minecraft:cardinal_direction"="south"]',
                         "setblock 0 65 2 redstone_block"],
            "comparator": ['setblock 0 65 1 unpowered_comparator ["minecraft:cardinal_direction"="south"]',
                           "setblock 0 65 2 redstone_block"],
            "parallel_repeater": ['setblock 0 65 1 unpowered_repeater ["minecraft:cardinal_direction"="west"]',
                                  "setblock -1 65 1 redstone_block"],
        }
        for name, commands in cases.items():
            reference.command("fill -3 63 -3 3 69 3 air")
            reference.command("fill -2 64 -2 2 64 3 stone")
            reference.command('setblock 0 65 0 unpowered_repeater ["minecraft:cardinal_direction"="west"]')
            for command in commands:
                reference.command(command)
            time.sleep(.6)
            reference.command("setblock -1 65 0 redstone_block")
            time.sleep(.6)
            powered = reference.matches((0, 65, 0), "powered_repeater")
            unpowered = reference.matches((0, 65, 0), "unpowered_repeater")
            if powered == unpowered:
                raise RuntimeError(f"repeater {name}: cannot identify native powered state")
            output["scenarios"].append({"side_source": name, "rear": 15, "powered": powered})
            print(json.dumps({"progress": name, "powered": powered}), flush=True)
        if not output["scenarios"][0]["powered"] or output["scenarios"][3]["powered"]:
            raise RuntimeError("repeater lock positive/negative controls failed")
        output["complete"] = True
    finally:
        reference.stop()
        output["commands"] = reference.commands
        output["log_directory"] = str(reference.root)
        output["build_log"] = [line for line in reference.log if "Version:" in line or "Build ID:" in line]
        args.output.write_text(json.dumps(output, indent=2) + "\n", encoding="utf-8")
        print(json.dumps({"output": str(args.output), "complete": output["complete"], "logs": str(reference.root)}))


if __name__ == "__main__":
    main()
