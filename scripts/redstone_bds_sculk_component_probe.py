#!/usr/bin/env python3
"""Read comparator samples around real sculk vibration arrivals in an owned BDS.

The native Script API callback holds its own work briefly while Python reads
the process. There is no injection, suspension, process write or patched BDS.
"""

import argparse
import hashlib
import itertools
import json
from pathlib import Path
import struct

from redstone_bds_client import LocalClient, network_state_hash
from redstone_bds_circuit_order_probe import Memory, inspect, read_circuit, torch_vtables
from redstone_bds_probe import Reference
from redstone_bds_sculk_probe import SCRIPT as BASE_SCRIPT
from redstone_native_probe import ClientPE


CASES = [{"sensor": sensor, "distance": 4, "request_phase": phase}
         for sensor, phase in itertools.product(("sculk_sensor", "calibrated_sculk_sensor"), (0, 1))]
POSITIONS = [(-23, 64, 0), (-24, 64, -1), (-25, 64, 0), (-24, 64, 1)]
SCRIPT = BASE_SCRIPT.replace('ready=true;emit({ready:true});', '''
  cmd("fill -26 61 -3 -22 68 3 air");cmd("setblock -24 64 0 stone");
  const pp=[[-23,64,0],[-24,64,-1],[-25,64,0],[-24,64,1]],ff=["west","south","east","north"];
  for(const p of pp)cmd(`setblock ${p[0]} ${p[1]+1} ${p[2]} stone`);
  cmd("setblock -24 65 0 redstone_wire");cmd('setblock -24 63 0 lever ["lever_direction"="down_north_south"]');
  for(let i=0;i<4;i++)cmd(`setblock ${pp[i].join(" ")} redstone_torch ["torch_facing_direction"="${ff[i]}"]`);
  ready=true;system.runTimeout(()=>emit({ready:true}),40);''')
SCRIPT = SCRIPT.replace('if(!current){\n', 'if(!current){\n  cmd("fill -26 61 -3 -22 68 3 air");\n')
SCRIPT = SCRIPT.replace('lastSample=system.currentTick;', '''lastSample=system.currentTick;
 const click=current.changes[0]?.tick;
 if(click&&system.currentTick-click>=2&&system.currentTick-click<=12){
  emit({checkpoint:true,scenario:c,sample:current.samples.at(-1)});
  const until=Date.now()+400;while(Date.now()<until){}
 }''')


def sample(memory, system, distance):
    memory.live = True
    before = memory.read(system + 8, 0x40)
    _, pointers = read_circuit(memory, system)
    result = {}
    for name, pos in (("sensor", (distance, 65, 0)), ("comparator", (distance + 1, 65, 0))):
        pointer = pointers[pos]
        raw = memory.read(pointer, 0xB0 if name == "comparator" else 0x60)
        result[name] = {"vtable_rva": hex(struct.unpack_from("<Q", raw)[0] - memory.base),
                        "strength": struct.unpack_from("<h", raw, 0x4E)[0]}
        if name == "comparator":
            result[name].update(analog=struct.unpack_from("<i", raw, 0x90)[0],
                                side_analog=struct.unpack_from("<i", raw, 0x94)[0],
                                rear=struct.unpack_from("<i", raw, 0xA4)[0],
                                side=struct.unpack_from("<i", raw, 0xA0)[0],
                                ready=bool(raw[0xA8]))
    if before != memory.read(system + 8, 0x40):
        raise RuntimeError("native component map changed during checkpoint read")
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "assets", "client", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--port", type=int, default=30865)
    args = parser.parse_args()
    vtables, locator = torch_vtables(ClientPE(args.binary), ClientPE(args.client))
    output = {"complete": False, "reference": __doc__, "locator": locator,
              "binary_sha256": hashlib.sha256(args.binary.read_bytes()).hexdigest(),
              "scenarios": [], "checkpoints": []}
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port,
                          script=SCRIPT.replace("CASES", json.dumps(CASES)), local_client=True)
    client = memory = None
    try:
        reference.until(lambda s: "ADAMANT_USE " in s and '"ready":true' in s, timeout=30)
        memory = Memory(reference.process)
        inspect(memory, vtables, set(POSITIONS))
        memory.live = True
        client = LocalClient(reference, args.port)
        reference.command("op RedstoneAudit")
        reference.command("scriptevent audit:start now")
        while True:
            lines = reference.until(lambda s: "ADAMANT_USE " in s, timeout=30)
            record = json.loads(lines[-1].split("ADAMANT_USE ", 1)[1].strip())
            if record.get("error"):
                raise RuntimeError(record)
            if "action" in record:
                record["action"]["runtime_id"] = network_state_hash(record["block_state"])
                client.send(record["action"])
            if record.get("checkpoint"):
                record["components"] = sample(memory, memory.system_address, record["scenario"]["distance"])
                output["checkpoints"].append(record)
                print(json.dumps(record), flush=True)
            if "scenario" in record and "samples" in record:
                output["scenarios"].append(record)
            if record.get("complete"):
                if record["count"] != len(CASES) or len(output["checkpoints"]) != len(CASES) * 11:
                    raise RuntimeError("incomplete sculk component checkpoints")
                output["complete"] = True
                break
    finally:
        if client:
            client.stop()
        if memory:
            memory.close()
        reference.stop()
        output["log_directory"] = str(reference.root)
        output["build_log"] = [s for s in reference.log if "Version:" in s or "Build ID:" in s]
        args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
        print(json.dumps({"output": str(args.output), "complete": output["complete"], "logs": str(reference.root)}), flush=True)
