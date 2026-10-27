#!/usr/bin/env python3
"""Execute client vibration selection, signal strength and dispatcher range gates.

The selector has no source actor; floorf is supplied by the host CRT. Sensor
activation is observed through a supplied callback. An authored ABI wrapper
runs the original dispatcher arithmetic span with a supplied radius query and
exits at its original accept/reject continuations. These are component
executions, not a native world or a full event dispatcher simulation.
"""

import argparse
import ctypes as C
from ctypes import wintypes as W
import hashlib
import itertools
import json
from pathlib import Path
import struct

from redstone_native_probe import ClientPE
from redstone_native_lightning_search_probe import NativeSearch, BASE


SPANS = {"selector": (0x25D8EC0, 0x2D4), "frequency": (0x25D7C70, 0x19),
         "sensor_receive": (0x4AE91E0, 0x9B), "signal_scale": (0xE61E154, 4),
         "dispatcher_range": (0x25D660D, 0x7E),
         "frequency_table": (0xE829988, 65*4),
         "unsigned_conversion": (0xE5E3600, 8), "sign_mask": (0xE6022E0, 16),
         "epsilon": (0xE77B9D0, 4)}


class NativeSelector(NativeSearch):
    def __init__(self, pe):
        self.pe, self.keep, self.committed = pe, [], set()
        self.kernel = C.WinDLL("kernel32", use_last_error=True)
        self.kernel.VirtualAlloc.argtypes = [C.c_void_p, C.c_size_t, W.DWORD, W.DWORD]
        self.kernel.VirtualAlloc.restype = C.c_void_p
        self.kernel.VirtualProtect.argtypes = [C.c_void_p, C.c_size_t, W.DWORD, C.POINTER(W.DWORD)]
        self.kernel.VirtualFree.argtypes = [C.c_void_p, C.c_size_t, W.DWORD]
        self.address = self.kernel.VirtualAlloc(BASE, 0x12000000, 0x2000, 1)
        if self.address != BASE:
            raise RuntimeError("preferred client range unavailable")
        for name, (rva, size) in SPANS.items():
            self.map(rva, pe.read(rva, size), executable=name in ("selector", "frequency", "sensor_receive", "dispatcher_range"))
        self.crt = C.CDLL("msvcrt")
        self.bridge(0xE5A6A90, C.cast(self.crt.floorf, C.c_void_p).value)
        self.call = C.WINFUNCTYPE(None, C.c_void_p, C.c_uint64, C.c_void_p,
                                 C.c_void_p, C.c_void_p)(BASE + 0x25D8EC0)
        self.inactive = self.callback(C.c_bool, [C.c_void_p, C.c_void_p], lambda *_: False)
        self.activate = self.callback(None, [C.c_void_p, C.c_void_p, C.c_void_p, C.c_int, C.c_int],
                                      lambda _source, _pos, _actor, strength, frequency:
                                      self.received.append({"strength": strength, "frequency": frequency}))
        self.bridge(0x81B4A50, C.cast(self.inactive, C.c_void_p).value)
        self.bridge(0x81B51D0, C.cast(self.activate, C.c_void_p).value)
        self.receive = C.WINFUNCTYPE(None, C.c_void_p, C.c_void_p, C.c_void_p,
                                    C.c_void_p, C.c_void_p, C.c_float, C.c_uint,
                                    C.c_void_p)(BASE + 0x4AE91E0)
        self.map(0xF0FBAA8, pe.read(0xF0FBAA8, 8))
        if pe.read(0xE585660, 2) != b"\xff\xe0":
            raise RuntimeError("unexpected indirect dispatcher")
        self.map(0xE585660, pe.read(0xE585660, 2), executable=True)
        self.get_radius = self.callback(C.c_uint, [C.c_void_p], lambda _: self.radius)
        vtable, listener, link = self.buffer(0x20), self.buffer(8), self.buffer(8)
        self.put(vtable, 0x18, "Q", C.cast(self.get_radius, C.c_void_p).value)
        self.put(listener, 0, "Q", C.addressof(vtable))
        self.put(link, 0, "Q", C.addressof(listener))
        self.range_link = link
        # The original instruction span needs rbp-relative positions, rdi's
        # listener link and xmm6's unsigned-conversion constant. Preserve the
        # nonvolatile registers at both original branch continuations.
        self.map(0x32CE100, struct.pack("<f", 2**63))
        self.map(0x32CE000, bytes.fromhex("0f287424200f287c2430440f284424404883c4505b5f5dc3"), executable=True)
        for rva, result in ((0x25D6580, b"\x31\xc0"), (0x25D668B, b"\xb8\x01\x00\x00\x00")):
            relative = 0x32CE000 - (rva + len(result) + 5)
            self.map(rva, result + b"\xe9" + struct.pack("<i", relative), executable=True)
        wrapper = (bytes.fromhex("5557534883ec500f297424200f297c2430440f29442440488be9488bfa")
                   + b"\x48\xb8" + struct.pack("<Q", BASE+0x32CE100) + bytes.fromhex("f30f1030")
                   + b"\x48\xb8" + struct.pack("<Q", BASE+0x25D660D) + b"\xff\xe0")
        self.map(0x32CF000, wrapper, executable=True)
        self.range_call = C.WINFUNCTYPE(C.c_bool, C.c_void_p, C.c_void_p)(BASE+0x32CF000)

    def in_range(self, origin, listener, radius):
        frame = self.buffer(0x80)
        self.put(frame, 8, "fff", *origin)
        self.put(frame, 0x40, "fff", *listener)
        self.radius = radius
        accepted = bool(self.range_call(C.addressof(frame)+0x50, C.addressof(self.range_link)))
        return {"origin": origin, "listener": listener, "radius": radius, "accepted": accepted}

    def signal(self, distance, radius):
        config, event = self.buffer(0x20), self.buffer(8)
        self.put(event, 0, "I", 1)
        self.received = []
        self.receive(C.addressof(config), None, None, C.addressof(event), None, distance, radius, None)
        if len(self.received) != 1:
            raise RuntimeError("native sensor did not activate exactly once")
        return {"distance": distance, "radius": radius, **self.received[0]}

    def run(self, listener, candidates):
        selector = self.buffer(0x48)
        self.put(selector, 0, "q", -1)
        receiver = (C.c_int * 3)(*listener)
        selected = []
        for candidate in candidates:
            event, context = self.buffer(8), self.buffer(0x20)
            self.put(event, 0, "I", candidate["event_id"])
            self.put(context, 0, "fff", *candidate["position"])
            self.call(C.addressof(selector), candidate["tick"], C.addressof(event),
                      C.addressof(context), C.addressof(receiver))
            event_ptr = struct.unpack_from("<Q", selector, 0x38)[0]
            selected.append({"tick": struct.unpack_from("<q", selector)[0],
                             "distance": struct.unpack_from("<f", selector, 8)[0],
                             "position": list(struct.unpack_from("<iii", selector, 0xC)),
                             "event_id": struct.unpack("<I", C.string_at(event_ptr, 4))[0]})
        return {"listener": listener, "candidates": candidates, "selected": selected}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--client", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != "8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("client differs from inspected 1.26.51.1 image")
    native = NativeSelector(pe)
    cases = []
    for listener in ([0, 65, 0], [17, 65, -17]):
        for offset in itertools.product((-3.99, -.01, .5, 1.99, 3.01), repeat=2):
            pos = [listener[0]+offset[0], 65.5, listener[2]+offset[1]]
            cases.append(native.run(listener, [{"tick": 100, "event_id": 1, "position": pos}]))
    for anchor, axis, delta in itertools.product((0,17,1000000,-1000000), range(3), (-1e-8,1e-8)):
        listener = [anchor,65,anchor]
        pos = [v+.5 for v in listener]
        pos[axis] = listener[axis]+1+delta
        cases.append(native.run(listener, [{"tick": 100, "event_id": 1, "position": pos}]))
    # Different Euclidean distances in one integer shell compete by frequency.
    for events in ((1, 27), (27, 1), (1, 1)):
        for positions in (([2.5, 65.5, .5], [2.5, 65.5, 1.5]),
                          ([2.5, 65.5, 1.5], [2.5, 65.5, .5])):
            candidates = [{"tick": 100, "event_id": event, "position": pos}
                          for event, pos in zip(events, positions)]
            candidates.append({"tick": 101, "event_id": 27, "position": [.5, 65.5, .5]})
            cases.append(native.run([0, 65, 0], candidates))
    signals = []
    for radius in (8, 16):
        distances = set(float(i) for i in range(radius+1))
        for n in range(1, 15):
            bits = struct.unpack("<I", struct.pack("<f", radius*n/15))[0]
            distances.update(struct.unpack("<f", struct.pack("<I", bits+offset))[0] for offset in (-1,0,1))
        signals.extend(native.signal(distance, radius) for distance in sorted(distances))
    ranges = []
    for listener in ([.5,65.5,.5], [1000000.5,65.5,-1000000.5]):
        for radius in (8,16):
            for offset in (0, -.01, .000001, .01, .03, .062, .063, .1, 1):
                for direction in (-1,1):
                    origin = [listener[0]+direction*(radius+offset), listener[1], listener[2]]
                    ranges.append(native.in_range(origin, listener, radius))
            for dx, dy, dz in ((0,0,0),(1,1,1),(radius,0,1),(0,radius,.1),(.1,0,radius)):
                ranges.append(native.in_range([listener[0]+dx,listener[1]+dy,listener[2]+dz],listener,radius))
    native.close()
    output = {"complete": True, "reference": __doc__, "client_sha256": digest,
              "spans": {name: {"rva": hex(rva), "size": size,
                         "sha256": hashlib.sha256(pe.read(rva, size)).hexdigest()}
                        for name, (rva, size) in SPANS.items()}, "scenarios": cases,
              "signals": signals}
    output["ranges"] = ranges
    args.output.write_text(json.dumps(output, separators=(",", ":"))+"\n", encoding="utf-8")
    print(json.dumps({"complete": True, "cases": len(cases), "signals": len(signals), "ranges": len(ranges)}))
