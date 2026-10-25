#!/usr/bin/env python3
"""Execute relocation-free redstone evaluators from a local Bedrock client PE.

No game binaries or machine code are redistributed. The output records the RVA
and SHA-256 of each executed function. Run in a separate 64-bit Windows Python
process; only the inspected, call-free evaluator bodies are copied and invoked.
The synthetic component buffers isolate arithmetic/pipeline behavior; this does
not execute CircuitSceneGraph, world ticks, block callbacks or rendering.
"""

import argparse
import ctypes
from ctypes import wintypes
import hashlib
import json
import random
from pathlib import Path
import struct
import sys


FUNCTIONS = {
    "ComparatorCapacitor::evaluate": (0x66D8710, 0xC1),
    "RepeaterCapacitor::evaluate": (0x66DB530, 0x2BC),
    "RedstoneTorchCapacitor::evaluate": (0x66D9C60, 0x46),
    "PulseCapacitor::evaluate": (0x66D93C0, 0x1B),
    "TransporterComponent::evaluate": (0x66DC420, 0x14),
}

INSPECTED_HASHES = {
    "ComparatorCapacitor::evaluate": "76279037e4380573611141549e782e743779714704f65b3fabf383d86c51edfc",
    "RepeaterCapacitor::evaluate": "1d7b4ce7750cbacb963c03d299615fa3b5b1227d4f81a54a6c6169f15acd0a45",
    "RedstoneTorchCapacitor::evaluate": "08484d458dd4bad69fa812712a02a08cc8f10766b526bb9b3ebf9485fdd194d6",
    "PulseCapacitor::evaluate": "2927e41be82161b6aa32ddaac496bd5c10dd2a288646f7024729b402283d5b2a",
    "TransporterComponent::evaluate": "380d115dc566bcf8dcad0bb0809c42241c5d6ea9624b000db4bed6472514f08e",
}


class ClientPE:
    def __init__(self, filename):
        self.filename = filename
        self.data = filename.read_bytes()
        pe = struct.unpack_from("<I", self.data, 0x3C)[0]
        if self.data[pe:pe + 4] != b"PE\0\0":
            raise ValueError("not a PE image")
        machine, count = struct.unpack_from("<HH", self.data, pe + 4)
        if machine != 0x8664:
            raise ValueError("expected an AMD64 client")
        optional = struct.unpack_from("<H", self.data, pe + 20)[0]
        start = pe + 24 + optional
        self.sections = []
        for i in range(count):
            size, rva, raw_size, offset = struct.unpack_from("<IIII", self.data, start + i * 40 + 8)
            self.sections.append((rva, size, raw_size, offset))

    def read(self, address, size):
        for rva, virtual_size, raw_size, offset in self.sections:
            if rva <= address and address + size <= rva + min(virtual_size, raw_size):
                start = offset + address - rva
                return self.data[start:start + size]
        raise ValueError(f"RVA {address:#x}+{size:#x} is outside backed PE sections")


class Evaluator:
    def __init__(self, code):
        self.kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        self.kernel.VirtualAlloc.argtypes = [ctypes.c_void_p, ctypes.c_size_t, wintypes.DWORD, wintypes.DWORD]
        self.kernel.VirtualAlloc.restype = ctypes.c_void_p
        self.kernel.VirtualProtect.argtypes = [ctypes.c_void_p, ctypes.c_size_t, wintypes.DWORD, ctypes.POINTER(wintypes.DWORD)]
        self.kernel.VirtualFree.argtypes = [ctypes.c_void_p, ctypes.c_size_t, wintypes.DWORD]
        self.address = self.kernel.VirtualAlloc(None, len(code), 0x3000, 0x04)
        if not self.address:
            raise ctypes.WinError(ctypes.get_last_error())
        ctypes.memmove(self.address, code, len(code))
        old = wintypes.DWORD()
        if not self.kernel.VirtualProtect(self.address, len(code), 0x20, ctypes.byref(old)):
            self.close()
            raise ctypes.WinError(ctypes.get_last_error())
        self.call = ctypes.WINFUNCTYPE(ctypes.c_bool, ctypes.c_void_p, ctypes.c_void_p, ctypes.c_void_p)(self.address)

    def __call__(self, component):
        return bool(self.call(ctypes.addressof(component), None, None))

    def close(self):
        if self.address:
            self.kernel.VirtualFree(self.address, 0, 0x8000)
            self.address = None


def put(component, offset, fmt, value):
    struct.pack_into(fmt, component, offset, value)


def get(component, offset, fmt):
    return struct.unpack_from(fmt, component, offset)[0]


def comparators(evaluate):
    cases = []
    for subtract in (False, True):
        for rear in range(16):
            for left in range(16):
                for right in range(16):
                    component = ctypes.create_string_buffer(0xC0)
                    put(component, 0x90, "<i", rear)
                    put(component, 0x94, "<i", right)
                    put(component, 0x98, "<i", left)
                    put(component, 0x9C, "<i", int(subtract))
                    changed = evaluate(component)
                    cases.append([int(subtract), rear, left, right, get(component, 0x4E, "<h"), int(changed)])
    # Analog sentinels choose the cached direct-wire strengths instead.
    direct = []
    for subtract in (False, True):
        for rear in range(16):
            for side in range(16):
                component = ctypes.create_string_buffer(0xC0)
                for offset in (0x90, 0x94, 0x98):
                    put(component, offset, "<i", -1)
                put(component, 0x9C, "<i", int(subtract))
                put(component, 0xA0, "<i", rear)
                put(component, 0xA4, "<i", side)
                evaluate(component)
                direct.append([int(subtract), rear, side, get(component, 0x4E, "<h")])
    return {"columns": ["subtract", "rear", "left", "right", "output", "changed"], "cases": cases,
            "direct_columns": ["subtract", "rear", "side", "output"], "direct_cases": direct}


def repeaters(evaluate, initial_states):
    traces = []
    inputs = [([0] * 3 + [1] * 8 + [0] * 8), ([0] * 3 + [1] + [0] * 15),
              ([0] * 3 + [1, 0] * 8), ([1] * 3 + [0] * 12)]
    for delay in range(4):
        for sequence in inputs:
            component = ctypes.create_string_buffer(0xC0)
            ctypes.memmove(ctypes.addressof(component) + 0x90, initial_states, 16)
            put(component, 0xA4, "<i", delay)
            output = []
            # Inject the cached input, not a fabricated world or source graph.
            # mPulseCount/mPulse/mNextPulse follow the inspected cacheValues
            # input-edge bookkeeping; evaluate itself is the native function.
            for power in sequence:
                component[0xA9] = bytes([power])
                previous = get(component, 0xB0, "<B")
                if previous == power:
                    put(component, 0xAC, "<i", 0)
                else:
                    count = get(component, 0xAC, "<i")
                    put(component, 0xAC, "<i", count + 1)
                    if count == 0:
                        put(component, 0xB1, "<B", previous ^ 1)
                    put(component, 0xB0, "<B", previous ^ 1)
                evaluate(component)
                output.append(get(component, 0xA8, "<B"))
            traces.append({"delay": delay, "input": sequence, "output": output})
    return traces


def torches(evaluate):
    cases = []
    for flags in range(64):
        for counter in (0, 7, 31, 32, 33, 34):
            for next_order in (0, 1):
                for reignite in (0, 1):
                    component = ctypes.create_string_buffer(0x98)
                    put(component, 0x78, "<Q", next_order)
                    put(component, 0x80, "<i", counter)
                    for bit in range(6):
                        put(component, 0x84 + bit, "<B", flags >> bit & 1)
                    put(component, 0x8A, "<B", reignite)
                    changed = evaluate(component)
                    cases.append([flags, counter, next_order, reignite,
                                  get(component, 0x84, "<B"), get(component, 0x85, "<B"),
                                  get(component, 0x86, "<B"), get(component, 0x80, "<i"), int(changed)])
    return {"columns": ["flags", "self_power_count", "next_order_present", "can_reignite",
                        "on", "half_frame", "changed", "new_self_power_count", "result"], "cases": cases}


def repeater_transitions(evaluate, initial_states):
    """Execute changing-delay/locking traces against the inspected evaluator.

    checkLock's side-list traversal is replaced by an injected boolean and its
    three inspected stage copies; cacheValues input-edge bookkeeping is also
    transcribed. The actual pipeline and pulse stretching execute native code.
    """
    rng = random.Random(0xADA26_51)
    traces = []
    for n in range(128):
        component = ctypes.create_string_buffer(0xC0)
        initially_powered = bool(n & 1)
        ctypes.memmove(ctypes.addressof(component) + 0x90, initial_states, 16)
        if initially_powered:
            for offset, value in zip((0x90, 0x94, 0x98, 0x9C), (3, 0, 0, 0)):
                put(component, offset, "<i", value)
            put(component, 0xA8, "<B", 1)
        rows = []
        power, locked, delay = 0, 0, n % 4
        for _ in range(96):
            if rng.randrange(3) == 0:
                power ^= 1
            if rng.randrange(11) == 0:
                locked ^= 1
            if rng.randrange(13) == 0:
                delay = rng.randrange(4)
            put(component, 0xA4, "<i", delay)
            put(component, 0xAA, "<B", locked)
            put(component, 0xA9, "<B", power)
            if locked:
                stage = get(component, 0x90, "<i")
                for offset in (0x94, 0x98, 0x9C):
                    put(component, offset, "<i", stage)
            else:
                previous = get(component, 0xB0, "<B")
                if previous == power:
                    put(component, 0xAC, "<i", 0)
                else:
                    count = get(component, 0xAC, "<i")
                    put(component, 0xAC, "<i", count + 1)
                    if count == 0:
                        put(component, 0xB1, "<B", previous ^ 1)
                    put(component, 0xB0, "<B", previous ^ 1)
            evaluate(component)
            rows.append([delay, power, locked, get(component, 0xA8, "<B")])
        traces.append({"initial_powered": initially_powered, "steps": rows})
    return {"seed": "0xADA2651", "columns": ["delay", "input", "locked", "output"], "traces": traces}


def simple_capacitors(pulse,transporter):
    pulses,transporters=[],[]
    for previous in range(16):
        for input_power in range(16):
            component=ctypes.create_string_buffer(0x88)
            put(component,0x4E,"<h",previous)
            put(component,0x60,"<i",input_power)
            put(component,0x79,"<B",int(input_power!=0))
            pulse(component)
            pulses.append([previous,input_power,get(component,0x4E,"<h"),get(component,0x60,"<i")])
            component=ctypes.create_string_buffer(0x88)
            put(component,0x4E,"<h",previous)
            put(component,0x60,"<i",input_power)
            changed=transporter(component)
            transporters.append([previous,input_power,get(component,0x4E,"<h"),int(changed)])
    return {"pulse_columns":["previous","input","strength","next_input"],"pulses":pulses,
            "transporter_columns":["previous","input","strength","changed"],"transporters":transporters}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if sys.platform != "win32" or struct.calcsize("P") != 8:
        parser.error("a 64-bit Windows Python process is required")
    pe = ClientPE(args.image)
    output = {"reference": "Bedrock Windows client 1.26.51.1", "image_sha256": hashlib.sha256(pe.data).hexdigest(),
              "functions": {}}
    evaluators = {}
    try:
        for name, (rva, size) in FUNCTIONS.items():
            code = pe.read(rva, size)
            if hashlib.sha256(code).hexdigest() != INSPECTED_HASHES[name]:
                raise ValueError(f"{name}: bytes differ from the inspected, relocation-free function")
            output["functions"][name] = {"rva": hex(rva), "size": size, "sha256": hashlib.sha256(code).hexdigest()}
            evaluators[name] = Evaluator(code)
        output["comparators"] = comparators(evaluators["ComparatorCapacitor::evaluate"])
        initial_states = pe.read(0xE69F2D0, 16)
        output["repeater_initial_states"] = list(struct.unpack("<4i", initial_states))
        output["repeaters"] = repeaters(evaluators["RepeaterCapacitor::evaluate"], initial_states)
        output["repeater_transitions"] = repeater_transitions(evaluators["RepeaterCapacitor::evaluate"], initial_states)
        output["torches"] = torches(evaluators["RedstoneTorchCapacitor::evaluate"])
        output["simple_capacitors"] = simple_capacitors(evaluators["PulseCapacitor::evaluate"],evaluators["TransporterComponent::evaluate"])
        args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
        print(json.dumps({"output": str(args.output), "comparator_cases": len(output["comparators"]["cases"]),
                          "direct_cases": len(output["comparators"]["direct_cases"]),
                          "repeater_traces": len(output["repeaters"])}))
    finally:
        for evaluator in evaluators.values():
            evaluator.close()


if __name__ == "__main__":
    main()
