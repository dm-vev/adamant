#!/usr/bin/env python3
"""Read native circuit traversal from an owned, isolated unmodified BDS.

The test pack drives a small torch circuit. ReadProcessMemory observes its
component objects and MSVC list/vector layout; no process writes, injection,
debugger, suspension or existing world are used. Heap reads are asynchronous:
component counters are diagnostic, not a tick-atomic simulation trace.
The inspected client evaluator's identical function body locates the BDS
vtable independently of the client's function addresses (BDS has no RTTI).
"""

import bisect
import argparse
import ctypes
from ctypes import wintypes
import hashlib
import json
import struct
from pathlib import Path

from redstone_bds_probe import Reference
from redstone_bds_torch_cluster_translation_probe import SCRIPT
from redstone_native_probe import ClientPE, FUNCTIONS, INSPECTED_HASHES


def occurrences(data, pattern):
    start = 0
    while (start := data.find(pattern, start)) >= 0:
        yield start
        start += 1


def rva_at(pe, offset):
    for rva, _, size, raw in pe.sections:
        if raw <= offset < raw + size:
            return rva + offset - raw
    raise ValueError("file offset outside PE sections")


def torch_vtables(pe, client):
    header = struct.unpack_from("<I", pe.data, 0x3C)[0]
    image_base = struct.unpack_from("<Q", pe.data, header + 24 + 24)[0]
    function = "RedstoneTorchCapacitor::evaluate"
    code = client.read(*FUNCTIONS[function])
    if hashlib.sha256(code).hexdigest() != INSPECTED_HASHES[function]:
        raise RuntimeError("client evaluator does not match inspected function")
    matches = list(occurrences(pe.data, code))
    if len(matches) != 1:
        raise RuntimeError("expected one identical native BDS torch evaluator")
    evaluate = rva_at(pe, matches[0])
    results = set()
    for pointer in occurrences(pe.data, struct.pack("<Q", image_base + evaluate)):
        vtable = rva_at(pe, pointer) - 0x80
        strength = struct.unpack("<Q", pe.read(vtable + 8, 8))[0] - image_base
        # Disassembled BDS getStrength: mState[0].mOn ? 15 : 0.
        if pe.read(strength, 18) != bytes.fromhex("31d280b98400000000b80f0000000f44c2c3"):
            continue
        results.add(vtable)
    if len(results) != 1:
        raise RuntimeError("no unique validated BDS torch vtable found")
    return sorted(results), {"client_sha256": hashlib.sha256(client.data).hexdigest(),
                            "evaluate_rva": hex(evaluate), "evaluate_sha256": INSPECTED_HASHES[function]}


class MemoryInfo(ctypes.Structure):
    _fields_ = [("BaseAddress", ctypes.c_void_p), ("AllocationBase", ctypes.c_void_p),
                ("AllocationProtect", wintypes.DWORD), ("PartitionId", wintypes.WORD),
                ("RegionSize", ctypes.c_size_t), ("State", wintypes.DWORD),
                ("Protect", wintypes.DWORD), ("Type", wintypes.DWORD)]


class Memory:
    def __init__(self, process):
        # The only accepted target is the child created by Reference.
        if process.poll() is not None:
            raise RuntimeError("owned BDS is not running")
        self.kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        self.kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        self.kernel.OpenProcess.restype = wintypes.HANDLE
        self.kernel.CloseHandle.argtypes = [wintypes.HANDLE]
        self.kernel.ReadProcessMemory.argtypes = [wintypes.HANDLE, ctypes.c_void_p, ctypes.c_void_p,
                                                 ctypes.c_size_t, ctypes.POINTER(ctypes.c_size_t)]
        self.kernel.VirtualQueryEx.argtypes = [wintypes.HANDLE, ctypes.c_void_p,
                                              ctypes.POINTER(MemoryInfo), ctypes.c_size_t]
        self.kernel.VirtualQueryEx.restype = ctypes.c_size_t
        self.kernel.K32EnumProcessModules.argtypes = [wintypes.HANDLE, ctypes.POINTER(ctypes.c_void_p),
                                                     wintypes.DWORD, ctypes.POINTER(wintypes.DWORD)]
        self.handle = self.kernel.OpenProcess(0x400 | 0x10, False, process.pid)
        if not self.handle:
            raise ctypes.WinError(ctypes.get_last_error())
        self.segments = []
        self.starts = []
        self.live = False
        try:
            modules = (ctypes.c_void_p * 1024)()
            needed = wintypes.DWORD()
            if not self.kernel.K32EnumProcessModules(self.handle, modules, ctypes.sizeof(modules), ctypes.byref(needed)):
                raise ctypes.WinError(ctypes.get_last_error())
            self.base = modules[0]
            header = self.read(self.base, 4096)
            if header[:2] != b"MZ":
                raise RuntimeError("main module is not a PE")
            pe = struct.unpack_from("<I", header, 0x3C)[0]
            self.size = struct.unpack_from("<I", header, pe + 24 + 56)[0]
        except BaseException:
            self.close()
            raise

    def read(self, address, size):
        data = ctypes.create_string_buffer(size)
        actual = ctypes.c_size_t()
        if not self.kernel.ReadProcessMemory(self.handle, address, data, size, ctypes.byref(actual)) or actual.value != size:
            raise OSError(f"cannot read owned BDS at {address:#x}+{size:#x}")
        return data.raw

    def snapshot(self):
        address = 0
        while address < 0x7FFFFFFFFFFF:
            info = MemoryInfo()
            if not self.kernel.VirtualQueryEx(self.handle, address, ctypes.byref(info), ctypes.sizeof(info)):
                break
            region_base = info.BaseAddress or 0
            end = region_base + info.RegionSize
            if end <= address:
                raise RuntimeError("VirtualQueryEx did not advance")
            # Committed readable private memory, excluding guard/no-access.
            if info.State == 0x1000 and info.Type == 0x20000 and info.Protect & 0xFF in (2, 4, 8, 0x20, 0x40, 0x80) and not info.Protect & 0x100:
                for part in range(region_base, end, 4 * 1024 * 1024):
                    try:
                        data = self.read(part, min(end - part, 4 * 1024 * 1024))
                    except OSError:
                        continue  # A concurrently released range is not evidence.
                    self.starts.append(part)
                    self.segments.append((part, data))
            address = end

    def cached(self, address, size):
        if self.live:
            return self.read(address, size)
        index = bisect.bisect_right(self.starts, address) - 1
        if index >= 0:
            base, data = self.segments[index]
            start = address - base
            if 0 <= start and start + size <= len(data):
                return data[start:start + size]
        raise ValueError("object not fully present in observed private memory")

    def find(self, pattern):
        for base, data in self.segments:
            for offset in occurrences(data, pattern):
                if (base + offset) & 7 == 0:
                    yield base + offset

    def close(self):
        if self.handle:
            self.kernel.CloseHandle(self.handle)
            self.handle = None


def component(memory, address):
    data = memory.cached(address, 0x68)
    vtable, = struct.unpack_from("<Q", data)
    if not memory.base <= vtable < memory.base + memory.size:
        raise ValueError("component vtable is outside the main module")
    pos = struct.unpack_from("<iii", data, 0x34)
    if any(abs(n) > 1000000 for n in pos) or data[0x55] != 0:
        raise ValueError("invalid/removed component")
    kind, = struct.unpack_from("<Q", data, 0x58)
    return {"pos": pos, "vtable_rva": hex(vtable - memory.base), "base_type": hex(kind)}


def vector_entry(memory, address):
    data = memory.cached(address, 24)
    pointer, x, y, z, flags = struct.unpack_from("<QiiiB", data)
    result = component(memory, pointer)
    if result["pos"] != (x, y, z) or flags & ~15:
        raise ValueError("not a chunk component vector entry")
    return {**result, "flags": flags}


def chunk_node(memory, address):
    data = memory.cached(address, 0x40)
    following, previous = struct.unpack_from("<QQ", data)
    x, y, z = struct.unpack_from("<iii", data, 0x10)
    evaluate = data[0x20]
    begin, end, capacity = struct.unpack_from("<QQQ", data, 0x28)
    if y != 0 or x % 16 or z % 16 or evaluate not in (0, 1) or not begin <= end <= capacity or not 0 <= end - begin <= 24 * 4096 or (end - begin) % 24:
        raise ValueError("not an active-chunk map node")
    entries = [vector_entry(memory, p) for p in range(begin, end, 24)]
    if any(e["pos"][0] >> 4 != x >> 4 or e["pos"][2] >> 4 != z >> 4 for e in entries):
        raise ValueError("vector entry is outside chunk key")
    return {"pos": (x, y, z), "evaluate": bool(evaluate), "entries": entries}, following, previous


def read_circuit(memory, system):
    data = memory.cached(system, 0x1B9)
    head, size = struct.unpack_from("<QQ", data, 0x50)
    mask, buckets = struct.unpack_from("<QQ", data, 0x78)
    if not 0 < size <= 256 or not 8 <= buckets <= 65536 or buckets & (buckets - 1) or mask != buckets - 1 or data[0x1B8] not in (0, 1):
        raise ValueError("not a validated circuit system")
    chunk_order = []
    p = struct.unpack("<Q", memory.cached(head, 8))[0]
    previous = head
    while p != head and len(chunk_order) <= size:
        node, following, back = chunk_node(memory, p)
        if back != previous:
            raise ValueError("chunk list back link disagrees")
        chunk_order.append(node)
        previous, p = p, following
    if len(chunk_order) != size or struct.unpack("<Q", memory.cached(head + 8, 8))[0] != previous:
        raise ValueError("incomplete chunk list")
    pending = memory.cached(system + 0xC8, 0x40)
    all_map = memory.cached(system + 8, 0x40)
    all_head, all_count = struct.unpack_from("<QQ", all_map, 8)
    if all_count > 65536:
        raise ValueError("invalid component map size")
    all_components, pointers = [], {}
    p = struct.unpack("<Q", memory.cached(all_head, 8))[0]
    while p != all_head and len(all_components) <= all_count:
        entry = memory.cached(p, 0x28)
        pointer = struct.unpack_from("<Q", entry, 0x20)[0]
        value = component(memory, pointer)
        key = struct.unpack_from("<iii", entry, 0x10)
        if value["pos"] != key:
            # Passive powered-block objects do not carry the map position.
            value["component_pos"], value["pos"] = value["pos"], key
        pointers[key] = pointer
        all_components.append(value)
        p = struct.unpack_from("<Q", entry)[0]
    if len(all_components) != all_count or len(pointers) != all_count:
        raise ValueError("incomplete all-component map")
    return {"bucket_count": buckets, "chunks": chunk_order,
            "pending_bucket_count": struct.unpack_from("<Q", pending, 0x38)[0],
            "pending_count": struct.unpack_from("<Q", pending, 0x10)[0],
            "all_bucket_count": struct.unpack_from("<Q", all_map, 0x38)[0],
            "all_components": all_components}, pointers


def inspect(memory, vtables, positions):
    memory.snapshot()
    torches = {}
    for rva in vtables:
        for address in memory.find(struct.pack("<Q", memory.base + rva)):
            try:
                state = component(memory, address)
                data = memory.cached(address, 0x98)
                if struct.unpack_from("<Q", data, 0x90)[0] != 0x200003:
                    continue
                if state["pos"] not in positions:
                    continue
                torches[address] = {**state, "counter": struct.unpack_from("<i", data, 0x80)[0],
                                    "on": bool(data[0x84]), "next_address": struct.unpack_from("<Q", data, 0x78)[0]}
            except (ValueError, struct.error):
                continue
    if len(torches) != 4 or {s["pos"] for s in torches.values()} != positions:
        raise RuntimeError(f"expected four live torch objects, found {list(torches.values())}")
    nodes = {}
    diagnostics = {"pointer_references": 0, "vector_entries": 0, "begin_references": 0, "entry_errors": [], "node_errors": []}
    for torch in torches:
        for entry in memory.find(struct.pack("<Q", torch)):
            diagnostics["pointer_references"] += 1
            try:
                vector_entry(memory, entry)
            except (ValueError, struct.error) as error:
                if len(diagnostics["entry_errors"]) < 16:
                    diagnostics["entry_errors"].append({"pos": torches[torch]["pos"], "reason": str(error),
                                                        "raw": memory.cached(entry, 24).hex()})
                continue
            diagnostics["vector_entries"] += 1
            begins = [entry]
            for i in range(1, 128):
                try:
                    vector_entry(memory, entry - i * 24)
                except (ValueError, struct.error):
                    break
                begins.append(entry - i * 24)
            for begin in begins:
                for reference in memory.find(struct.pack("<Q", begin)):
                    diagnostics["begin_references"] += 1
                    address = reference - 0x28
                    try:
                        node, _, _ = chunk_node(memory, address)
                    except (ValueError, struct.error) as error:
                        if len(diagnostics["node_errors"]) < 12:
                            try:
                                raw = memory.cached(address, 0x40).hex()
                            except ValueError:
                                raw = None
                            diagnostics["node_errors"].append({"reason": str(error), "raw_node": raw})
                        continue
                    nodes[address] = node
    if not nodes:
        raise RuntimeError("no validated chunk component vectors found: " + json.dumps(diagnostics))
    # Follow the MSVC map's list, identifying its sentinel by structural checks.
    rings = set()
    for start in nodes:
        ring, p = [], start
        for _ in range(256):
            if p in ring:
                if p == start:
                    rings.add(tuple(sorted(ring)))
                break
            ring.append(p)
            try:
                following, previous = struct.unpack("<QQ", memory.cached(p, 16))
                if struct.unpack("<Q", memory.cached(following + 8, 8))[0] != p:
                    break
            except (ValueError, struct.error):
                break
            p = following
    systems = []
    root_diagnostics = []
    for ring in rings:
        for head in ring:
            for reference in memory.find(struct.pack("<Q", head)):
                system = reference - 0x50
                try:
                    data = memory.cached(system, 0x1B9)
                    size = struct.unpack_from("<Q", data, 0x58)[0]
                    mask, buckets = struct.unpack_from("<QQ", data, 0x78)
                    if size == len(ring) - 1:
                        root_diagnostics.append({"size": size, "mask": mask, "buckets": buckets, "flag": data[0x1B8]})
                    if size != len(ring) - 1 or not 8 <= buckets <= 65536 or buckets & (buckets - 1) or mask != buckets - 1 or data[0x1B8] not in (0, 1):
                        continue
                    circuit, _ = read_circuit(memory, system)
                    systems.append((system, circuit))
                except (ValueError, struct.error) as error:
                    if root_diagnostics and "reason" not in root_diagnostics[-1]:
                        root_diagnostics[-1]["reason"] = str(error)
                    continue
    if len(systems) != 1:
        raise RuntimeError(f"expected one validated CircuitSystem, found {len(systems)}; nodes={len(nodes)}, rings={len(rings)}, roots={root_diagnostics}")
    memory.system_address = systems[0][0]
    for state in torches.values():
        following = state.pop("next_address")
        if following not in torches:
            raise RuntimeError("torch dependency does not point at another live torch")
        state["next_pos"] = torches[following]["pos"]
    return {"torch_vtable_rvas": [hex(rva) for rva in vtables], "torches": list(torches.values()),
            "circuit": systems[0][1], "observed_private_bytes": sum(len(data) for _, data in memory.segments)}


def inspect_known(memory, system, vtables, positions):
    memory.live = True
    circuit, pointers = read_circuit(memory, system)
    torches = {}
    for pos in positions:
        address = pointers[pos]
        state = component(memory, address)
        data = memory.read(address, 0x98)
        if int(state["vtable_rva"], 16) not in vtables or struct.unpack_from("<Q", data, 0x90)[0] != 0x200003:
            raise RuntimeError("sampled position is not a validated native torch")
        torches[address] = {**state, "counter": struct.unpack_from("<i", data, 0x80)[0], "on": bool(data[0x84]),
                            "next_address": struct.unpack_from("<Q", data, 0x78)[0]}
    for state in torches.values():
        following = state.pop("next_address")
        if following not in torches:
            raise RuntimeError("torch dependency points outside the sampled cluster")
        state["next_pos"] = torches[following]["pos"]
    return {"torch_vtable_rvas": [hex(rva) for rva in vtables], "torches": list(torches.values()), "circuit": circuit}


def read_pending(memory, system):
    descriptor = memory.read(system + 0xC8, 0x40)
    head, count = struct.unpack_from("<QQ", descriptor, 8)
    if not 0 < count <= 256:
        raise RuntimeError("pending checkpoint was missed or map size is invalid")
    entries = []
    p = struct.unpack("<Q", memory.read(head, 8))[0]
    while p != head and len(entries) <= count:
        entry = memory.read(p, 0x40)
        pointer = struct.unpack_from("<Q", entry, 0x28)[0]
        vtable = struct.unpack("<Q", memory.read(pointer, 8))[0]
        if not memory.base <= vtable < memory.base + memory.size:
            raise RuntimeError("pending component vtable is outside the main module")
        entries.append({"pos": struct.unpack_from("<iii", entry, 0x10), "vtable_rva": hex(vtable - memory.base)})
        p = struct.unpack_from("<Q", entry)[0]
    if len(entries) != count or memory.read(system + 0xC8, 0x40) != descriptor:
        raise RuntimeError("pending map changed during checkpoint read")
    return {"bucket_count": struct.unpack_from("<Q", descriptor, 0x38)[0], "entries": entries}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "assets", "output", "client"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--port", type=int, default=30861)
    parser.add_argument("--suite", choices=("smoke", "ordering", "growth"), default="smoke")
    parser.add_argument("--pending-checkpoint", action="store_true",
                        help="hold the owned script callback for 500 ms after setup to read pending entries")
    parser.add_argument("--case-limit", type=int, choices=range(1, 21))
    args = parser.parse_args()
    pe = ClientPE(args.binary)
    vtables, locator = torch_vtables(pe, ClientPE(args.client))
    print(json.dumps({"torch_vtables": [hex(rva) for rva in vtables]}), flush=True)
    script = SCRIPT
    if args.suite in ("smoke", "growth"):
        original = 'for(const center of [[0,64,0],[8,64,8],[16,64,16],[-1,64,-1],[31,70,15]])for(const reverse of [false,true])for(const phase of [0,1])cases.push({sides:["east","north","west","south"],center,reverse,phase});'
        if script.count(original) != 1:
            raise RuntimeError("native fixture template changed")
        replacement = 'cases.push({sides:["east","north","west","south"],center:[0,64,0],reverse:false,phase:0});'
        if args.suite == "growth":
            replacement = 'for(const [count,reverse,phase] of [[0,false,0],[9,false,0],[70,false,1],[160,false,0],[70,true,1],[0,true,0]])cases.push({sides:["east","north","west","south"],center:[0,64,0],reverse,phase,count});'
        script = script.replace(original, replacement)
    if args.suite == "growth":
        script = script.replace('function setup(){', '''let staticPositions=[];
function setup(){
    for(const p of staticPositions){cmd(`setblock ${p.join(" ")} air`);cmd(`setblock ${p[0]} ${p[1]-1} ${p[2]} air`);}
    staticPositions=[];
''')
        original = 'start=system.currentTick;samples=[];'
        replacement = '''
    const grid=[];for(let x=-12;x<=44;x+=4)for(let z=-12;z<=28;z+=4)grid.push([x,81,z]);
    for(const p of grid.slice(0,c.count)){cmd(`setblock ${p[0]} ${p[1]-1} ${p[2]} stone`);cmd(`setblock ${p.join(" ")} lever ["lever_direction"="up_north_south","open_bit"=false]`);staticPositions.push(p);}
    start=system.currentTick;samples=[];'''
        if script.count(original) != 1:
            raise RuntimeError("native setup timing template changed")
        script = script.replace(original, replacement)
        script = script.replace('positions:positions.map(offset),samples,changes',
                                'positions:positions.map(offset),static_positions:staticPositions,samples,changes')
        original = 'if(!dimension.getBlock({x:-3,y:64,z:-3})||!dimension.getBlock({x:3,y:64,z:3}))'
        script = script.replace(original, 'if(!dimension.getBlock({x:-12,y:81,z:-12})||!dimension.getBlock({x:44,y:81,z:28})||!dimension.getBlock({x:-3,y:64,z:-3})||!dimension.getBlock({x:3,y:64,z:3}))')
    if args.case_limit:
        script = script.replace('let dimension,index=', f'cases.splice({args.case_limit});let dimension,index=')
    # Keep a finished geometry unchanged until Python has observed its lists.
    # The scoreboard handshake changes no redstone component or block.
    original = 'cmd("gamerule randomtickspeed 0");'
    script = script.replace(original, original + 'cmd("scoreboard objectives add audit_gate dummy");cmd("scoreboard players set advance audit_gate 0");')
    original = 'if(waiting){if((system.currentTick&1)===c.phase)setup();return;}'
    if script.count(original) != 1:
        raise RuntimeError("native waiting template changed")
    script = script.replace(original, 'if(waiting){if(world.scoreboard.getObjective("audit_gate").getScore("advance")<index)return;if((system.currentTick&1)===c.phase)setup();return;}')
    if args.pending_checkpoint:
        # The declaration uses waiting=false too, but only setup ends this way.
        original = 'changes=[{tick:start,kind:"setup"}];waiting=false;'
        if script.count(original) != 1:
            raise RuntimeError("native setup checkpoint template changed")
        script = script.replace(original, original + 'emit({pending:index});const until=Date.now()+500;while(Date.now()<until){}')
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port, script=script)
    output = {"reference": "read-only heap inspection of owned unmodified Windows BDS", "complete": False,
              "binary_sha256": hashlib.sha256(pe.data).hexdigest(), "atomic": False, "locator": locator,
              "scenarios": []}
    memory = None
    system_address = None
    pending_observations = {}
    expected = min({"smoke": 1, "ordering": 20, "growth": 6}[args.suite], args.case_limit or 20)
    try:
        while True:
            lines = reference.until(lambda line: "ADAMANT_TIMING " in line, timeout=45)
            record = json.loads(lines[-1].split("ADAMANT_TIMING ", 1)[1].strip())
            if "error" in record:
                raise RuntimeError(record)
            if "pending" in record and system_address is not None:
                memory = Memory(reference.process)
                pending_observations[record["pending"]] = read_pending(memory, system_address)
                memory.close()
                memory = None
            if "scenario" in record:
                memory = Memory(reference.process)
                positions = {tuple(p) for p in record["positions"]}
                if system_address is None:
                    observation = inspect(memory, vtables, positions)
                    system_address = memory.system_address
                else:
                    observation = inspect_known(memory, system_address, vtables, positions)
                memory.close()
                memory = None
                index = len(output["scenarios"])
                if args.pending_checkpoint and index > 0:
                    if index not in pending_observations:
                        raise RuntimeError("missing pending-map checkpoint")
                    observation["pending_before_flush"] = pending_observations[index]
                output["scenarios"].append({"world_trace": record, **observation})
                print(json.dumps({"scenario": record["scenario"], "chunks": len(observation["circuit"]["chunks"]),
                                  "chunk_buckets": observation["circuit"]["bucket_count"],
                                  "pending_buckets": observation["circuit"]["pending_bucket_count"]}), flush=True)
                if len(output["scenarios"]) < expected:
                    # The next native marker is the acknowledgement. A console
                    # fence could consume that marker before the read loop.
                    reference.process.stdin.write("scoreboard players set advance audit_gate " + str(len(output["scenarios"])) + "\n")
                    reference.process.stdin.flush()
            if record.get("complete"):
                if record["count"] != expected or len(output["scenarios"]) != expected:
                    raise RuntimeError("incomplete circuit traversal capture")
                break
        output["complete"] = True
    finally:
        if memory:
            memory.close()
        reference.stop()
        output["log_directory"] = str(reference.root)
        output["build_log"] = [line for line in reference.log if "Version:" in line or "Build ID:" in line]
        args.output.write_text(json.dumps(output, indent=2) + "\n", encoding="utf-8")
        print(json.dumps({"output": str(args.output), "complete": output["complete"], "logs": str(reference.root)}), flush=True)


if __name__ == "__main__":
    main()
