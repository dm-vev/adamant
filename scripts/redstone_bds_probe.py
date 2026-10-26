#!/usr/bin/env python3
"""Run isolated, console-driven redstone scenarios on an unmodified local BDS.

All world/config/log writes go to a fresh temporary directory. Read-only asset
directories are junctions to --assets. No existing server/world is started or
modified. Retained logs and JSON identify the native build and every command.
"""

import argparse
import hashlib
import json
from pathlib import Path
import queue
import shutil
import subprocess
import tempfile
import threading
import time
import uuid


class Reference:
    def __init__(self, binary, assets, port, script=None, structures=None, local_client=False):
        self.root = Path(tempfile.mkdtemp(prefix="adamant-redstone-bds-"))
        shutil.copyfile(binary, self.root / "bedrock_server.exe")
        for name in ("behavior_packs", "resource_packs", "definitions", "data", "world_templates"):
            target, source = self.root / name, assets / name
            if not source.exists():
                continue
            quote = lambda p: "'" + str(p).replace("'", "''") + "'"
            subprocess.run(["powershell", "-NoProfile", "-Command",
                            f"New-Item -ItemType Junction -Path {quote(target)} -Target {quote(source)} | Out-Null"],
                           check=True, creationflags=subprocess.CREATE_NO_WINDOW)
        for name in ("bedrock_runtime_data", "profanity_filter.wlist"):
            if (assets / name).is_file():
                shutil.copyfile(assets / name, self.root / name)
        settings = {"server-name": "RedstoneReference", "online-mode": "true", "allow-cheats": "true",
                    "default-player-permission-level": "operator", "transport": "raknet", "server-ip": "127.0.0.1",
                    "server-port": str(port), "server-portv6": str(port + 1), "view-distance": "4", "tick-distance": "4",
                    "max-threads": "2", "level-name": "redstone-reference", "level-seed": "1", "level-type": "FLAT",
                    "content-log-file-enabled": "true", "pause-when-empty-seconds": "0",
                    "allow-list": "true", "enable-lan-visibility": "false"}
        if local_client:
            # This opt-in affects only the fresh temporary reference server.
            # The packet client has no Xbox credentials. BDS forbids an
            # allowlist without online authentication.
            settings["online-mode"] = "false"
            settings["allow-list"] = "false"
            settings["gamemode"] = "creative"
            settings["default-player-permission-level"] = "member"
        (self.root / "server.properties").write_text("".join(f"{k}={v}\n" for k, v in settings.items()), encoding="utf-8")
        (self.root / "permissions.json").write_text("[]\n", encoding="utf-8")
        (self.root / "allowlist.json").write_text("[]\n", encoding="utf-8")
        if script is not None:
            permissions = self.root / "config" / "default"
            permissions.mkdir(parents=True)
            (permissions / "permissions.json").write_text(
                json.dumps({"allowed_modules": ["@minecraft/server"]}), encoding="utf-8")
            # Never write into behavior_packs: it is a junction to user assets.
            pack = self.root / "development_behavior_packs" / "redstone_audit"
            (pack / "scripts").mkdir(parents=True)
            header_id, module_id = str(uuid.uuid4()), str(uuid.uuid4())
            manifest = {"format_version": 2, "header": {
                "name": "Redstone audit", "description": "Read native block states once per game tick",
                "uuid": header_id, "version": [1, 0, 0], "min_engine_version": [1, 21, 80]},
                "modules": [{"type": "script", "language": "javascript", "uuid": module_id,
                             "version": [1, 0, 0], "entry": "scripts/main.js"}],
                "dependencies": [{"module_name": "@minecraft/server", "version": "2.0.0"}]}
            (pack / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
            (pack / "scripts" / "main.js").write_text(script, encoding="utf-8")
            for name, contents in (structures or {}).items():
                # Authored audit structures live only in this temporary pack.
                if any(part in ("", ".", "..") or not part.replace("_", "").isalnum()
                       for part in name.split("/")):
                    raise ValueError("invalid audit structure name")
                target = pack / "structures" / (name + ".mcstructure")
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(contents)
            level = self.root / "worlds" / "redstone-reference"
            level.mkdir(parents=True)
            (level / "world_behavior_packs.json").write_text(
                json.dumps([{"pack_id": header_id, "version": [1, 0, 0]}]), encoding="utf-8")
        self.lines = queue.Queue()
        self.log = []
        self.commands = []
        self.process = subprocess.Popen([str(self.root / "bedrock_server.exe")], cwd=self.root,
                                        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                        text=True, encoding="utf-8", errors="replace", creationflags=subprocess.CREATE_NO_WINDOW)
        self.thread = threading.Thread(target=self.consume, daemon=True)
        self.thread.start()
        try:
            self.until(lambda line: "Server started." in line, 60)
        except Exception:
            self.stop()
            raise

    def consume(self):
        for line in self.process.stdout:
            self.log.append(line.rstrip())
            self.lines.put(line)

    def until(self, predicate, timeout=15):
        deadline = time.monotonic() + timeout
        result = []
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                raise RuntimeError("BDS stopped: " + "\n".join(self.log[-15:]))
            try:
                line = self.lines.get(timeout=.1)
            except queue.Empty:
                continue
            result.append(line.rstrip())
            if predicate(line):
                return result
        raise TimeoutError("BDS command timeout: " + "\n".join(self.log[-15:]))

    def command(self, command):
        marker = "AUDIT_END_" + uuid.uuid4().hex
        # Chat sent with /say is not echoed to BDS stdout without players.
        # An intentionally invalid /list argument is a visible FIFO fence.
        self.process.stdin.write(command + "\nlist " + marker + "\n")
        self.process.stdin.flush()
        lines = [line for line in self.until(lambda line: marker in line) if marker not in line]
        self.commands.append({"command": command, "response": lines})
        if any("Syntax error" in line or "Unknown command" in line for line in lines):
            raise RuntimeError("invalid reference command: " + repr(self.commands[-1]))
        return lines

    def matches(self, position, block, states=None):
        props = ""
        if states:
            props = " [" + ",".join(json.dumps(k) + "=" + json.dumps(v) for k, v in states.items()) + "]"
        lines = self.command(f"testforblock {' '.join(map(str, position))} {block}{props}")
        return any("Successfully found" in line for line in lines)

    def stop(self):
        if self.process.poll() is None:
            self.process.stdin.write("stop\n")
            self.process.stdin.flush()
            try:
                self.process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()
        self.thread.join(timeout=5)
        (self.root / "bds.log").write_text("\n".join(self.log) + "\n", encoding="utf-8")


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
        reference.command("gamerule dodaylightcycle false")
        # Native piston facing is reversed on ALL horizontal axes: 2 is
        # south, 3 north, 4 east, 5 west (unlike ordinary Facing values).
        offsets = {0: (0, -1, 0), 1: (0, 1, 0), 2: (0, 0, 1), 3: (0, 0, -1), 4: (1, 0, 0), 5: (-1, 0, 0)}
        for facing, direction in offsets.items():
            head = [direction[0], 65 + direction[1], direction[2]]
            sources = [("direct_" + str(face), (offset[0], 65 + offset[1], offset[2])) for face, offset in offsets.items()]
            sources += [("two_above", (0, 67, 0)), ("above_side", (0, 66, 1))]
            for name, power in sources:
                reference.command("fill -3 63 -3 3 69 3 air")
                reference.command(f'setblock 0 65 0 piston ["facing_direction"={facing}]')
                reference.command(f"setblock {' '.join(map(str, power))} redstone_block")
                time.sleep(.6)
                extended = reference.matches(head, "piston_arm_collision")
                output["scenarios"].append({"kind": "piston_power", "name": str(facing) + "/" + name,
                                            "facing_direction": facing, "power": list(power),
                                            "head": head, "extended": extended})
            if not any(case["extended"] for case in output["scenarios"][-8:]):
                raise RuntimeError(f"piston {facing} has no working positive control; capture is invalid")
            print(json.dumps({"progress": "piston", "facing": facing, "scenarios": len(output["scenarios"])}), flush=True)
        reference.command("fill -3 64 -3 3 69 3 air")
        reference.command("fill -2 64 -2 2 64 3 stone")
        reference.command('setblock 0 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west","output_subtract_bit"=true]')
        reference.command('setblock 0 65 1 unpowered_repeater ["minecraft:cardinal_direction"="south"]')
        reference.command("setblock -1 65 0 redstone_block")
        reference.command("setblock 0 65 2 redstone_block")
        time.sleep(1)
        for block in ("unpowered_comparator", "powered_comparator"):
            for lit in (False, True):
                matched = reference.matches((0, 65, 0), block, {"output_lit_bit": lit})
                if matched:
                    output["scenarios"].append({"kind": "comparator_subtract_equal", "rear": 15, "side": 15,
                                                "block": block, "lit": lit})
        output["commands"] = reference.commands
        if not any(case["kind"] == "comparator_subtract_equal" for case in output["scenarios"]):
            raise RuntimeError("comparator state did not match any queried native variant")
        output["complete"] = True
    finally:
        reference.stop()
        output["log_directory"] = str(reference.root)
        output["build_log"] = [line for line in reference.log if "Version:" in line or "Build ID:" in line]
        args.output.write_text(json.dumps(output, indent=2) + "\n", encoding="utf-8")
        print(json.dumps({"output": str(args.output), "scenarios": len(output["scenarios"]), "logs": str(reference.root)}))


if __name__ == "__main__":
    main()
