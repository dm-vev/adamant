#!/usr/bin/env python3
"""Execute the client's inspected torch cache against synthetic source lists.

The original cache body is copied unchanged. Its only external reference is
the CFG virtual-call dispatcher; an isolated JMP RAX trampoline routes those
calls to injected strength/type/group getters. No scene graph or world is
fabricated by this probe: source lists and dependency order are explicit input.
"""

import argparse
import ctypes
from ctypes import wintypes
import hashlib
import json
from pathlib import Path
import random
import struct
import sys

from redstone_native_probe import ClientPE, Evaluator, get, put


RVA, SIZE = 0x66D97D0, 0x485
EXPECTED = "1a1947129ebcad932566810cb2fd31c19a8915805cba7cc52f45bd165d24db86"
CFG_RVA = 0xF0FBAA8
TORCH, CAPACITOR, PRODUCER, POWERED_BLOCK = 0x200003, 0x200000, 0x80000, 0x40000
POSITION = (11, 64, 7)


class Cache:
    def __init__(self, code):
        self.kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        self.kernel.VirtualAlloc.argtypes = [ctypes.c_void_p, ctypes.c_size_t, wintypes.DWORD, wintypes.DWORD]
        self.kernel.VirtualAlloc.restype = ctypes.c_void_p
        self.kernel.VirtualProtect.argtypes = [ctypes.c_void_p, ctypes.c_size_t, wintypes.DWORD, ctypes.POINTER(wintypes.DWORD)]
        self.kernel.VirtualFree.argtypes = [ctypes.c_void_p, ctypes.c_size_t, wintypes.DWORD]
        page_rva = RVA & ~4095
        self.address = self.kernel.VirtualAlloc(None, CFG_RVA - page_rva + 4096, 0x2000, 0x01)
        if not self.address:
            raise ctypes.WinError(ctypes.get_last_error())
        self.dispatch = None
        try:
            self.dispatch = Evaluator(bytes.fromhex("ff e0"))  # Synthetic CFG dispatcher only.
            code_page = self.kernel.VirtualAlloc(self.address, 4096, 0x1000, 0x04)
            cfg_offset = CFG_RVA - page_rva
            pointer_page = self.kernel.VirtualAlloc(self.address + (cfg_offset & ~4095), 4096, 0x1000, 0x04)
            if code_page != self.address or pointer_page != self.address + (cfg_offset & ~4095):
                raise ctypes.WinError(ctypes.get_last_error())
            self.entry = self.address + RVA - page_rva
            ctypes.memmove(self.entry, code, len(code))
            ctypes.c_void_p.from_address(self.address + cfg_offset).value = self.dispatch.address
            old = wintypes.DWORD()
            if not self.kernel.VirtualProtect(self.address, 4096, 0x20, ctypes.byref(old)):
                raise ctypes.WinError(ctypes.get_last_error())
            self.call = ctypes.WINFUNCTYPE(None, ctypes.c_void_p, ctypes.c_void_p, ctypes.c_void_p)(self.entry)
        except BaseException:
            self.close()
            raise

    def __call__(self, component):
        position = (ctypes.c_int * 3)(*POSITION)
        self.call(ctypes.addressof(component), None, ctypes.addressof(position))

    def close(self):
        if self.dispatch:
            self.dispatch.close()
            self.dispatch = None
        if self.address:
            self.kernel.VirtualFree(self.address, 0, 0x8000)
            self.address = None


class Graph:
    def __init__(self):
        self.buffers = []
        self.getters = [ctypes.WINFUNCTYPE(ctypes.c_int, ctypes.c_void_p)(
            lambda p: ctypes.c_int.from_address(p + 0xB0).value),
            ctypes.WINFUNCTYPE(ctypes.c_uint64, ctypes.c_void_p)(
                lambda p: ctypes.c_uint64.from_address(p + 0xB8).value),
            ctypes.WINFUNCTYPE(ctypes.c_uint64, ctypes.c_void_p)(
                lambda p: ctypes.c_uint64.from_address(p + 0xC0).value)]
        self.vtable = ctypes.create_string_buffer(0xE0)
        for offset, callback in zip((0x8, 0xD0, 0xD8), self.getters):
            put(self.vtable, offset, "<Q", ctypes.cast(callback, ctypes.c_void_p).value)

    def component(self, data):
        component = ctypes.create_string_buffer(0xC8)
        self.buffers.append(component)
        put(component, 0, "<Q", ctypes.addressof(self.vtable))
        put(component, 0xB0, "<i", data.get("power", 0))
        put(component, 0xB8, "<Q", data.get("type", 0))
        put(component, 0xC0, "<Q", data.get("group", 0))
        children = data.get("sources", [])
        if children:
            entries = ctypes.create_string_buffer(len(children) * 32)
            self.buffers.append(entries)
            for i, child in enumerate(children):
                source = self.component(child)
                offset = i * 32
                put(entries, offset, "<Q", ctypes.addressof(source))
                put(entries, offset + 8, "<i", child.get("distance", 0))
                struct.pack_into("<iii", entries, offset + 12, *child.get("pos", (0, 0, 0)))
                put(entries, offset + 25, "<B", int(child.get("direct", True)))
            put(component, 8, "<Q", ctypes.addressof(entries))
            put(component, 16, "<Q", ctypes.addressof(entries) + len(entries))
        return component


def run_case(cache, evaluate, data):
    graph = Graph()
    component = graph.component({"sources": data["sources"]})
    for offset, value in zip((0x80, 0x84, 0x85, 0x86, 0x8A), data["initial"]):
        put(component, offset, "<i" if offset == 0x80 else "<B", value)
    chain = [component]
    for counter in data.get("chain", []):
        other = graph.component({})
        put(other, 0x80, "<i", counter)
        put(chain[-1], 0x78, "<Q", ctypes.addressof(other))
        chain.append(other)
    if len(chain) > 1:
        put(chain[-1], 0x78, "<Q", ctypes.addressof(chain[0]))
    cache(component)
    cached = [get(component, offset, "<i" if offset == 0x80 else "<B") for offset in (0x80, 0x87, 0x88, 0x89, 0x8A)]
    changed = evaluate(component)
    result = [get(component, offset, "<i" if offset == 0x80 else "<B") for offset in (0x80, 0x84, 0x85, 0x86, 0x8A)]
    return {**data, "cached": cached, "result": result, "changed": changed,
            "chain_result": [get(other, 0x80, "<i") for other in chain[1:]]}


def cases():
    own = {"type": TORCH, "group": CAPACITOR, "pos": POSITION}
    external = {"type": 0x200004, "group": CAPACITOR, "pos": (9, 64, 7)}
    for counter in (0, 7, 14, 15, 16, 32, 33, 34):
        for on in (0, 1):
            for self_power, external_power in ((0, 0), (0, 15), (15, 0), (15, 15), (14, 15), (15, 14)):
                sources = [{"group": POWERED_BLOCK, "sources": [{**own, "power": self_power}, {**external, "power": external_power}]}]
                for order in (False, True):
                    yield {"initial": [counter, on, 0, 0, 0], "sources": sources, "chain": [counter] * 3 if order else []}
    rng = random.Random(0x26_51_70)
    for _ in range(2048):
        sources = []
        for i in range(rng.randrange(5)):
            source = {"group": rng.choice((PRODUCER, CAPACITOR, POWERED_BLOCK, 0x20000)), "power": rng.randrange(16), "sources": []}
            for j in range(rng.randrange(5)):
                source["sources"].append({"group": rng.choice((PRODUCER, CAPACITOR)),
                    "type": rng.choice((TORCH, 0x200004, 0)), "power": rng.randrange(16), "distance": rng.randrange(17),
                    "pos": POSITION if rng.randrange(3) == 0 else (i, 64, j), "direct": bool(rng.randrange(2))})
            sources.append(source)
        yield {"initial": [rng.randrange(36), rng.randrange(2), rng.randrange(2), rng.randrange(2), rng.randrange(2)],
               "sources": sources, "chain": [rng.randrange(36) for _ in range(rng.choice((0, 0, 1, 2, 4, 6)))]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if sys.platform != "win32" or struct.calcsize("P") != 8:
        parser.error("64-bit Windows required")
    pe = ClientPE(args.image)
    code = pe.read(RVA, SIZE)
    if hashlib.sha256(code).hexdigest() != EXPECTED:
        raise ValueError("client torch cache is not the inspected version")
    evaluate_code = pe.read(0x66D9C60, 0x46)
    if hashlib.sha256(evaluate_code).hexdigest() != "08484d458dd4bad69fa812712a02a08cc8f10766b526bb9b3ebf9485fdd194d6":
        raise ValueError("client torch evaluator is not the inspected version")
    cache, evaluate = Cache(code), Evaluator(evaluate_code)
    try:
        result = {"reference": "Bedrock Windows client 1.26.51.1", "image_sha256": hashlib.sha256(pe.data).hexdigest(),
            "cache_rva": hex(RVA), "cache_sha256": EXPECTED, "source_position": POSITION,
            "evaluate_rva": "0x66d9c60", "evaluate_sha256": hashlib.sha256(evaluate_code).hexdigest(),
            "seed": "0x265170", "virtual_getters": "injected strength/type/group; all other cache instructions unchanged",
            "scope": "explicit synthetic source lists and dependency rings; no world, scene-graph construction or tick cadence",
            "state_columns": ["self_power_count", "on", "half_frame", "changed", "can_reignite"],
            "cases": [run_case(cache, evaluate, case) for case in cases()]}
        args.output.write_text(json.dumps(result, separators=(",", ":")) + "\n", encoding="utf-8")
        print(json.dumps({"output": str(args.output), "cases": len(result["cases"])}))
    finally:
        cache.close()
        evaluate.close()


if __name__ == "__main__":
    main()
