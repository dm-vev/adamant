#!/usr/bin/env python3
"""Read entity physics components and block item-escape flags from owned BDS.

All process access is read-only. Original-client instruction prefixes locate
the BDS actor vtables independently. Script callbacks hold bounded checkpoints;
entity IDs and live component bytes are validated. Script snapshots and actor
simulation may differ in phase; their agreement is recorded explicitly.
"""

import argparse
import hashlib
import json
from pathlib import Path
import struct
import time

from redstone_bds_circuit_order_probe import Memory, occurrences, rva_at
from redstone_bds_probe import Reference
from redstone_native_probe import ClientPE


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_ENTITY_PHYSICS "+JSON.stringify(x));
let d,ready=false,running=false,start=0,entities=[];
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start"){start=system.currentTick;running=true;}});
system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");d.runCommand("tickingarea add 0 0 0 63 100 31 audit true");d.runCommand("gamerule domobspawning false");d.runCommand("gamerule randomtickspeed 0");return;}
 if(!ready){if(!d.getBlock({x:63,y:65,z:31}))return;
  for(let i=0;i<3;i++){
   const x=8+(i%4)*16,z=8+Math.floor(i/4)*16;d.runCommand(`fill ${x-3} 63 ${z-3} ${x+3} 72 ${z+3} air`);d.runCommand(`fill ${x-3} 64 ${z-3} ${x+3} 64 ${z+3} stone`);
   const p={x:x+.5,y:65.25,z:z+.5};const e=i===2?d.spawnEntity("minecraft:armor_stand",p):d.spawnItem(new ItemStack("minecraft:diamond",1),p);e.clearVelocity();entities.push(e);
  }
  ready=true;emit({ready:true,entities:entities.map(e=>({id:e.id,type:e.typeId,position:e.location}))});return;
 }
 if(!running)return;
 const t=system.currentTick-start;
 if(t===1){d.runCommand("setblock 24 65 8 stone");
  for(const [n,p,v] of [[1,{x:24.5,y:65.25,z:8.5},{x:0,y:0,z:0}]]){
   entities[n].remove();entities[n]=d.spawnItem(new ItemStack("minecraft:diamond",1),p);entities[n].clearVelocity();entities[n].applyImpulse(v);
  }
 }
 if(t<=4){const deadline_ms=Date.now()+5000;emit({checkpoint:true,refresh_actors:t===1,tick:system.currentTick,deadline_ms,entities:entities.map(e=>({id:e.id,type:e.typeId,position:e.location,velocity:e.getVelocity()}))});while(Date.now()<deadline_ms){} }
 if(t===4){emit({complete:true});running=false;}
}catch(e){emit({error:String(e),stack:e.stack});}},1);
'''


def locate_actor(bds, client, rva):
    prefix = client.read(rva, 0x21 if rva == 0x33496D0 else 0x40)
    found = list(occurrences(bds.data, prefix))
    if len(found) != 1:
        raise RuntimeError("actor method prefix not unique")
    method = rva_at(bds, found[0])
    refs = list(occurrences(bds.data, struct.pack("<Q", 0x140000000 + method)))
    if len(refs) != 1:
        raise RuntimeError("actor virtual method reference not unique")
    slot = rva_at(bds, refs[0])
    # The vtables are adjacent with no sentinel. Walking backwards across code
    # pointers incorrectly includes preceding classes. These Actor slots also
    # match the native constructor's lea/mov-vptr assignments.
    table = slot-(0x130 if rva == 0x33496D0 else 0xC0)
    return table, dict(client_rva=hex(rva), prefix_size=len(prefix), prefix_sha256=hashlib.sha256(prefix).hexdigest(),
                       bds_method_rva=hex(method), bds_vtable_rva=hex(table), slot=hex(slot-table))


def component(memory, registry, entity, key, size):
    buckets, end = struct.unpack("<QQ", memory.read(registry+0x48, 16))
    count = (end-buckets)//8
    if count <= 0 or count & (count-1):
        raise RuntimeError("invalid entity registry bucket vector")
    entry = struct.unpack("<q", memory.read(buckets+8*((count-1)&key), 8))[0]
    entries, entries_end = struct.unpack("<QQ", memory.read(registry+0x68, 16))
    for _ in range(10000):
        if entry == -1:
            return None
        address = entries+entry*32
        if not entries <= address < entries_end:
            raise RuntimeError("invalid entity registry entry")
        raw = memory.read(address, 32)
        if struct.unpack_from("<I", raw, 8)[0] == key:
            storage = struct.unpack_from("<Q", raw, 16)[0]
            break
        entry = struct.unpack_from("<q", raw)[0]
    else:
        raise RuntimeError("entity registry chain does not terminate")
    if not storage:
        return None
    pages, pages_end = struct.unpack("<QQ", memory.read(storage+8, 16))
    page = (entity & 0x3FFFF) >> 11
    if pages+page*8 >= pages_end:
        return None
    sparse = struct.unpack("<Q", memory.read(pages+page*8, 8))[0]
    if not sparse:
        return None
    dense = struct.unpack("<I", memory.read(sparse+4*(entity&0x7FF), 4))[0]
    if (dense ^ (entity&0xFFFC0000)) > 0x3FFFE:
        return None
    dense &= 0x3FFFF
    if size == 0:
        return b""
    packed = struct.unpack("<Q", memory.read(storage+0x50, 8))[0]
    page = struct.unpack("<Q", memory.read(packed+8*(dense>>7), 8))[0]
    return memory.read(page+(dense&127)*size, size)


def read_actor(memory, address, vtable, is_item):
    raw = memory.read(address, 0x228)
    if struct.unpack_from("<Q", raw)[0] != memory.base+vtable:
        raise RuntimeError("native actor identity changed")
    registry, entity = struct.unpack_from("<QI", raw, 0x10)
    state, body = struct.unpack_from("<QQ", raw, 0x218)
    state_bytes, body_bytes = memory.read(state,36), memory.read(body,24)
    dep = component(memory, registry, entity, 0x256B41F6, 56)
    request = component(memory, registry, entity, 0x4C8F7288, 152)
    optional = component(memory, registry, entity, 0xC67426F3, 24)
    result = dict(position=struct.unpack_from("<3f", state_bytes),
                  velocity=struct.unpack_from("<3f", state_bytes,24),
                  body=struct.unpack("<6f", body_bytes),
                  actor_data_flags=None if optional is None else optional.hex())
    result["on_ground"] = component(memory,registry,entity,0xC29078A0,0) is not None
    if is_item:
        result["item_age"]=struct.unpack("<i",memory.read(address+0x448,4))[0]
    if dep:
        result["depenetration"] = dict(bits=struct.unpack_from("<I", dep)[0], minimum=struct.unpack_from("<3f", dep, 4),
            temporary_override=struct.unpack_from("<3f", dep, 40) if dep[52] else None)
    if request:
        result["move_request"] = dict(before=struct.unpack_from("<3f", request, 48), speed=struct.unpack_from("<3f", request, 60),
            max_depenetration=struct.unpack_from("<3f", request, 72), penetrating=bool(request[84]))
    if raw != memory.read(address, 0x228) or dep != component(memory, registry, entity, 0x256B41F6, 56) or state_bytes != memory.read(state,36) or body_bytes != memory.read(body,24):
        raise RuntimeError("native actor changed during checkpoint read")
    return result


def find_actor(memory, entry, table):
    found=[]
    for address in memory.find(struct.pack("<Q",memory.base+table)):
        try:
            raw=memory.read(address,0x228); state=struct.unpack_from("<Q",raw,0x218)[0]
            position=struct.unpack("<3f",memory.read(state,12))
            if any(abs(position[axis]-entry["position"][name])>2 for axis,name in enumerate(("x","y","z"))):
                continue
            registry, entity=struct.unpack_from("<QI",raw,0x10)
            unique=component(memory,registry,entity,0x18F957AF,8)
            if unique is not None and str(struct.unpack("<q",unique)[0])==entry["id"]:
                found.append(address)
        except (OSError,ValueError):
            continue
    if len(found)!=1: raise RuntimeError(f"native actor ID lookup not unique: {entry}: {found}")
    return found[0],table,entry["type"]=="minecraft:item"


def native_string(memory, address):
    raw = memory.read(address, 32)
    size, capacity = struct.unpack_from("<QQ", raw, 16)
    if size > 200 or capacity < size:
        raise ValueError("invalid native string")
    return (raw[:size] if capacity < 16 else memory.read(struct.unpack_from("<Q", raw)[0], size)).decode("ascii")


def all_block_flags(memory, expected):
    # Namespace is an inline MSVC string shared by every vanilla BlockType.
    # Scan once, then validate both names and the runtime vptr in each object.
    found = {}
    for namespace in memory.find(b"minecraft\0"):
        address = namespace-0xC0
        try:
            table = struct.unpack("<Q", memory.cached(address,8))[0]
            if not memory.base <= table < memory.base+memory.size:
                continue
            if native_string(memory,namespace)!="minecraft": continue
            name=native_string(memory,address+0xE8)
            if name not in expected or native_string(memory,address+0x98)!=name.split(":")[1]: continue
            raw=memory.read(address+0x130,0x38)
            if name in found: raise RuntimeError("duplicate native block type "+name)
            found[name]=dict(properties=struct.unpack_from("<Q",raw)[0],flags_164=raw[0x34],
                            pushes_out_items=bool(raw[0x34]&4),vtable_rva=hex(table-memory.base))
        except (OSError,ValueError,UnicodeDecodeError):
            continue
    missing=expected-set(found)
    if missing: raise RuntimeError("missing native block types: "+str(sorted(missing)))
    return found


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "assets", "client", "output"):
        parser.add_argument("--"+name, type=Path, required=True)
    parser.add_argument("--port", type=int, default=30884)
    args = parser.parse_args()
    bds, client = ClientPE(args.binary), ClientPE(args.client)
    hashes = dict(binary_sha256=hashlib.sha256(bds.data).hexdigest(), client_sha256=hashlib.sha256(client.data).hexdigest())
    if hashes != dict(binary_sha256="76d547f82e02c18d0986c30b47132c9cc4171d0f2df1c00649e50ff35788b321",client_sha256="8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5"):
        raise RuntimeError("reference image differs")
    item, item_locator = locate_actor(bds,client,0x33496D0)
    stand, stand_locator = locate_actor(bds,client,0x204BB30)
    output = dict(complete=False, reference=__doc__, **hashes, item_locator=item_locator, stand_locator=stand_locator, checkpoints=[])
    reference = Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=SCRIPT,local_client=True)
    memory = None
    try:
        line=reference.until(lambda s:"ADAMANT_ENTITY_PHYSICS " in s,timeout=45)[-1]
        ready=json.loads(line.split("ADAMANT_ENTITY_PHYSICS ",1)[1])
        if not ready.get("ready"):
            raise RuntimeError(ready)
        memory=Memory(reference.process)
        memory.snapshot()
        names=json.loads((Path(__file__).resolve().parent.parent/"server/block/testdata/bedrock_1_26_51_piston_movability.json").read_text(encoding="utf-8"))
        output["block_types"]=all_block_flags(memory,{b["name"] for b in names["blocks"]})
        actors=[]
        for entry in ready["entities"]:
            table=item if entry["type"]=="minecraft:item" else stand
            # Initial actors have settled while the immutable registry is read.
            entry={**entry,"position":{**entry["position"],"y":65}}
            actors.append(find_actor(memory,entry,table))
        reference.command("scriptevent audit:start now")
        while True:
            line=reference.until(lambda s:"ADAMANT_ENTITY_PHYSICS " in s,timeout=45)[-1]
            record=json.loads(line.split("ADAMANT_ENTITY_PHYSICS ",1)[1])
            if record.get("error"): raise RuntimeError(record)
            if record.get("checkpoint"):
                if time.time()*1000>=record["deadline_ms"]: raise RuntimeError("native checkpoint expired")
                if record.get("refresh_actors"):
                    memory.segments.clear();memory.starts.clear();memory.snapshot()
                    actors=[find_actor(memory,e,item if e["type"]=="minecraft:item" else stand) for e in record["entities"]]
                record["native"]=[read_actor(memory,*actor) for actor in actors]
                for script, live in zip(record["entities"],record["native"]):
                    offset=.125 if script["type"]=="minecraft:item" else 0
                    live["matches_script_position"]=all(abs(live["position"][axis]-script["position"][name]-(offset if axis==1 else 0))<.00002 for axis,name in enumerate(("x","y","z")))
                if time.time()*1000>=record["deadline_ms"]: raise RuntimeError("native read exceeded checkpoint")
                output["checkpoints"].append(record)
            if record.get("complete"):
                output["complete"]=True
                break
    finally:
        if memory: memory.close()
        reference.stop()
        output["log_directory"]=str(reference.root)
        args.output.write_text(json.dumps(output,indent=2)+"\n",encoding="utf-8")
        print(json.dumps(dict(complete=output["complete"], checkpoints=len(output["checkpoints"]), logs=str(reference.root))))
