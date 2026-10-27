"""Own a loopback packet client for one disposable native reference server."""

import json
from pathlib import Path
import queue
import subprocess
import threading
import time

from redstone_audit_structure import _payload, byte, compound, integer, string


def network_state_hash(state):
    """Canonical network hash of the native SDK's block state (not a Go RID)."""
    properties = {}
    for key, value in sorted(state["states"].items()):
        if isinstance(value, bool):
            properties[key] = byte(int(value))
        elif isinstance(value, int):
            properties[key] = integer(value)
        elif isinstance(value, str):
            properties[key] = string(value)
        else:
            raise ValueError("unsupported native block property")
    data = b"\x0a\0\0" + _payload(10, {"name": string(state["id"]), "states": compound(properties)})
    value = 2166136261
    for entry in data:
        value = ((value ^ entry) * 16777619) & 0xFFFFFFFF
    return value


class LocalClient:
    def __init__(self, reference, port, executable=None):
        self.root = reference.root
        if executable is None:
            executable = self.root / "redstone-client.exe"
            result = subprocess.run(["go", "build", "-o", str(executable), "./scripts/redstone_client"],
                                    cwd=Path(__file__).resolve().parents[1], capture_output=True, text=True,
                                    timeout=60, creationflags=subprocess.CREATE_NO_WINDOW)
            if result.returncode:
                raise RuntimeError("audit client build failed: " + result.stdout + result.stderr)
        self.process = subprocess.Popen([str(executable), f"127.0.0.1:{port}"], stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
                                        encoding="utf-8", errors="replace", creationflags=subprocess.CREATE_NO_WINDOW)
        self.lines, self.log = queue.Queue(), []
        self.thread = threading.Thread(target=self.consume, daemon=True)
        self.thread.start()
        try:
            deadline = time.monotonic() + 40
            while time.monotonic() < deadline:
                if self.process.poll() is not None:
                    raise RuntimeError("audit client exited: " + "\n".join(self.log[-12:]))
                try:
                    line = self.lines.get(timeout=.1)
                except queue.Empty:
                    continue
                if line.startswith("{") and json.loads(line).get("ready"):
                    return
            raise TimeoutError("audit client did not finish spawning")
        except Exception:
            self.stop()
            raise

    def consume(self):
        for line in self.process.stdout:
            self.log.append(line.rstrip())
            self.lines.put(line.rstrip())

    def send(self, action):
        if self.process.poll() is not None:
            raise RuntimeError("audit client stopped: " + "\n".join(self.log[-12:]))
        if "held_count" in action:
            deadline = time.monotonic() + 5
            while True:
                held = None
                for line in reversed(self.log):
                    try:
                        record = json.loads(line)
                    except (ValueError, TypeError):
                        continue
                    if "held" in record:
                        held = record["held"]["Stack"]
                        break
                if held and held["Count"] == action["held_count"] and held["NetworkID"] == action["held_network_id"]:
                    break
                if time.monotonic() >= deadline:
                    raise TimeoutError("client did not receive the expected held stack: " + str(held))
                if self.process.poll() is not None:
                    raise RuntimeError("audit client stopped while waiting for its held stack")
                time.sleep(.02)
        self.process.stdin.write(json.dumps(action) + "\n")
        self.process.stdin.flush()

    def stop(self):
        if self.process.poll() is None:
            try:
                self.send({"action": "stop"})
                self.process.wait(timeout=8)
            except (OSError, RuntimeError, subprocess.TimeoutExpired):
                if self.process.poll() is None:
                    self.process.kill()
                self.process.wait()
        self.thread.join(timeout=2)
        (self.root / "client.log").write_text("\n".join(self.log) + "\n", encoding="utf-8")
