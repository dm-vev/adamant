#!/usr/bin/env python3
"""Record native chunk actor RNG inputs with hardware execution breakpoints.

Only the fresh Reference-owned BDS is debugged. Original instruction bytes and
assets are unchanged. Debug registers are restored before detach. These traces
are debugger-observed evidence, separate from the uninstrumented API fixtures.
"""
import argparse
import hashlib
import json
from pathlib import Path
import struct
import time

from redstone_bds_circuit_order_probe import Memory, occurrences, rva_at
from redstone_bds_level_random_probe import SCRIPT, NativeCoreRandom, locate_level_random, random_vtable
from redstone_bds_probe import Reference
from redstone_native_probe import ClientPE
from redstone_owned_hardware_trace import OwnedHardwareTrace


def actor_tick(bds, client, core):
    header = struct.unpack_from('<I', bds.data, 0x3C)[0]
    pdata, size = struct.unpack_from('<II', bds.data, header + 24 + 112 + 3 * 8)
    ranges = {begin: end for begin, end, _ in struct.iter_unpack('<III', bds.read(pdata, size))}
    candidates = []
    for offset in occurrences(bds.data, client.read(0x32CB470, 35)):
        begin = rva_at(bds, offset)
        if begin not in ranges:
            continue
        code = bds.read(begin, ranges[begin] - begin)
        calls = [begin + i + 5 for i in range(len(code) - 4)
                 if code[i] == 0xE8 and begin + i + 5 + struct.unpack_from('<i', code, i + 1)[0] == core]
        if len(calls) == 1:
            candidates.append((begin, calls[0]))
    if len(candidates) != 1:
        raise RuntimeError(f'expected unique actor tick with direct shuffle RNG call, got {candidates}')
    return candidates[0]


def held_slot_script(script):
    from redstone_bds_dispenser_switch_pulse_probe import replace_once
    script = replace_once(script, 'let d,loaded=0,ready=false,start=0,offset=0,cells=[];',
        'let d,loaded=0,ready=false,start=0,offset=0,cells=[],requested=false;'
        'system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")requested=true;});')
    script = replace_once(script, 'if(system.currentTick&1)return;ready=true;setup();return;}',
        'if(!requested){if(!ready){ready=true;emit({ready:true,tick:system.currentTick});}return;}'
        'if(system.currentTick&1)return;setup();ready=true;return;}')
    script = replace_once(script, 'if(!ready){for(let x=', 'if(start===0){for(let x=')
    return replace_once(script, ' if(t===128){',
        ' const deadline_ms=Date.now()+200;emit({checkpoint:true,tick:system.currentTick,deadline_ms});'
        'while(Date.now()<deadline_ms){}\n if(t===128){')


def brewing_script(pull=False):
    from redstone_bds_hopper_slot_transfer_probe import slot_script
    from redstone_bds_dispenser_switch_pulse_probe import replace_once
    cases = [dict(kind='hopper', face=face, target='brewing_stand', fill=pattern, phase=0,
                  initial_source=[dict(id='minecraft:diamond', count=4), dict(id='minecraft:' + item, count=count)] + [None] * 3,
                  initial_target=[None] * 5)
             for face in (0, 2, 3, 4, 5)
             for pattern, item, count in (
                 ('reagent', 'nether_wart', 4), ('fuel', 'blaze_powder', 4),
                 ('potion', 'potion', 1), ('bottle', 'glass_bottle', 4))]
    if pull:
        from redstone_bds_hopper_slot_pull_probe import pull_script
        from redstone_bds_hopper_brewing_pull_probe import brewing_pull_cases
        script = pull_script().replace('CASES', json.dumps(brewing_pull_cases()))
    else:
        script = slot_script().replace('CASES', json.dumps(cases))
    return held_slot_script(script), 20, 'ADAMANT_TIMING '


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('binary', 'assets', 'client', 'output'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--port', type=int, default=30874)
    parser.add_argument('--brewing', action='store_true')
    parser.add_argument('--brewing-pull', action='store_true')
    parser.add_argument('--dropper-mixed', action='store_true')
    parser.add_argument('--crafter-ejection', action='store_true')
    parser.add_argument('--dispenser-ejection', action='store_true')
    parser.add_argument('--note-events', action='store_true')
    parser.add_argument('--note-load', action='store_true')
    parser.add_argument('--bell-ring', action='store_true')
    parser.add_argument('--weather', action='store_true')
    parser.add_argument('--piston-cross-chunk', action='store_true')
    parser.add_argument('--areas', action='store_true')
    parser.add_argument('--circle-radius', type=int, choices=(1,2,4))
    args = parser.parse_args()
    bds, client = ClientPE(args.binary), ClientPE(args.client)
    if hashlib.sha256(client.data).hexdigest() != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    if hashlib.sha256(bds.data).hexdigest() != '76d547f82e02c18d0986c30b47132c9cc4171d0f2df1c00649e50ff35788b321':
        raise RuntimeError('BDS differs from inspected hardware-context layout')
    table, locator = random_vtable(bds, client)
    core_rva = int(locator['core_rva'], 16)
    chunk_rva, shuffle_caller = actor_tick(bds, client, core_rva)
    locator.update(chunk_actor_tick=hex(chunk_rva), shuffle_caller=hex(shuffle_caller))
    script, count, marker = brewing_script(args.brewing_pull) if args.brewing or args.brewing_pull else (SCRIPT, 0, 'ADAMANT_LEVEL_RANDOM ')
    frames, moving_rva, area_rva = (256 if count else 20), None, None
    container_callers={}
    ejection_callers=set()
    structures=None
    item_tick_rva=None
    random_float_caller=None
    block_event_rva=None
    bell_ring_rva=None
    if args.bell_ring:
        from redstone_bds_bell_pulse_probe import SCRIPT as BELL_SCRIPT,bell_cases
        from redstone_bds_dispenser_switch_pulse_probe import replace_once
        cases=bell_cases()
        script=BELL_SCRIPT.replace('CASES',json.dumps(cases))
        script=replace_once(script,'let d,loaded=0,ready=false,start=0,cells=[],serial=0;',
            'let d,loaded=0,ready=false,start=0,cells=[],serial=0,requested=false;'
            'system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")requested=true;});')
        script=replace_once(script,'if(!ready){if(!get(','if(start===0){if(!get(')
        script=replace_once(script,'  if(system.currentTick%20!==2)return;',
            '  if(!requested){if(!ready){ready=true;emit({ready:true,tick:system.currentTick});}return;}'
            'if(system.currentTick%20!==2)return;')
        script=replace_once(script,' for(const c of cells){const name="audit_bell_snap_"+serial++;\n'
            '  if(!cmd(`structure save ${name} ${c.base.join(" ")} ${c.base.join(" ")} false disk true`).successCount)throw new Error("bell actor snapshot failed");\n'
            '  c.samples.push({tick:system.currentTick,block:state(c.base),source:state(c.source),snapshot:"mystructure:"+name});',
            ' for(const c of cells){c.samples.push({tick:system.currentTick,block:state(c.base),source:state(c.source)});')
        script=replace_once(script,' if(t===112){',
            ' const deadline_ms=Date.now()+200;emit({checkpoint:true,tick:system.currentTick,deadline_ms});'
            'while(Date.now()<deadline_ms){}\n if(t===112){')
        matches=[rva_at(bds,p) for p in occurrences(bds.data,client.read(0x2DB8720,0x26))
                 if bds.read(rva_at(bds,p)+0x37,0x14)==client.read(0x2DB8757,0x14)]
        if len(matches)!=1:
            raise RuntimeError('BDS bell ring entry/layout is ambiguous')
        bell_ring_rva=matches[0];locator['bell_ring_rva']=hex(bell_ring_rva)
        count,frames,marker=len(cases),112,'ADAMANT_TIMING '
    if args.note_events or args.note_load:
        from redstone_bds_note_pulse_probe import SCRIPT as NOTE_SCRIPT,note_cases,note_structures
        if args.note_load:
            from redstone_bds_note_load_probe import note_load_script,note_load_cases as note_cases,note_load_structures as note_structures
            NOTE_SCRIPT=note_load_script()
        from redstone_bds_dispenser_switch_pulse_probe import replace_once
        cases=note_cases();structures=note_structures()
        script=NOTE_SCRIPT.replace('CASES',json.dumps(cases))
        script=replace_once(script,'let d,loaded=0,ready=false,start=0,cells=[];',
            'let d,loaded=0,ready=false,start=0,cells=[],requested=false;'
            'system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")requested=true;});')
        script=replace_once(script,'if(!ready){if(!get(','if(start===0){if(!get(')
        script=replace_once(script,'  if(system.currentTick%20!==2)return;',
            '  if(!requested){if(!ready){ready=true;emit({ready:true,tick:system.currentTick});}return;}'
            'if(system.currentTick%20!==2)return;')
        script=replace_once(script,' if(t===128){',
            ' const deadline_ms=Date.now()+200;emit({checkpoint:true,tick:system.currentTick,deadline_ms});'
            'while(Date.now()<deadline_ms){}\n if(t===128){')
        matches=[rva_at(bds,p) for p in occurrences(bds.data,client.read(0x2D84440,0x17))]
        if len(matches)!=1:raise RuntimeError('BDS blockEvent locator is ambiguous')
        block_event_rva=matches[0]
        locator['block_event_rva']=hex(block_event_rva)
        count,frames,marker=len(cases),128,'ADAMANT_TIMING '
    if args.crafter_ejection or args.dispenser_ejection:
        from redstone_bds_crafter_ejection_probe import SCRIPT as EJECTION_SCRIPT,ejection_cases,ejection_structures
        if args.dispenser_ejection:
            from redstone_bds_dispenser_ejection_probe import ejection_script,ejection_cases,ejection_structures
            EJECTION_SCRIPT=ejection_script()
        from redstone_bds_dispenser_switch_pulse_probe import replace_once
        cases=ejection_cases();structures=ejection_structures()
        script=EJECTION_SCRIPT.replace('CASES',json.dumps(cases))
        script=replace_once(script,'let d,loaded=0,ready=false,start=0,cells=[];',
            'let d,loaded=0,ready=false,start=0,cells=[],requested=false;'
            'system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")requested=true;});')
        script=replace_once(script,'if(!ready){for(let x=','if(start===0){for(let x=')
        script=replace_once(script,'}if(system.currentTick%20!==2)return;ready=true;setup();return;}',
            '}if(!requested){if(!ready){ready=true;emit({ready:true,tick:system.currentTick});}return;}'
            'if(system.currentTick%20!==2)return;ready=true;setup();return;}')
        script=replace_once(script,' if(t===100){',
            ' const deadline_ms=Date.now()+200;emit({checkpoint:true,tick:system.currentTick,deadline_ms});'
            'while(Date.now()<deadline_ms){}\n if(t===100){')
        count,frames,marker=len(cases),100,'ADAMANT_TIMING '
        matches=[rva_at(bds,p) for p in occurrences(bds.data,client.read(0x97882D0,47))]
        if matches!=[0x8A231C0] or bds.read(0x8A232FC,15)!=client.read(0x978840C,15):
            raise RuntimeError('BDS ejection frame layout differs')
        header=struct.unpack_from('<I',bds.data,0x3c)[0]
        pdata,size=struct.unpack_from('<II',bds.data,header+24+112+24)
        end=next(end for begin,end,_ in struct.iter_unpack('<III',bds.read(pdata,size)) if begin==matches[0])
        code=bds.read(matches[0],end-matches[0])
        ejection_callers={matches[0]+i+5 for i in range(len(code)-4) if code[i]==0xe8 and matches[0]+i+5+struct.unpack_from('<i',code,i+1)[0]==core_rva}
        if len(ejection_callers)!=6:raise RuntimeError('BDS ejection Gaussian callers differ')
        float_matches=[rva_at(bds,p) for p in occurrences(bds.data,client.read(0xDA7390,13))
                       if bds.read(rva_at(bds,p)+33,9)==client.read(0xDA73B1,9)]
        if len(float_matches)!=1:raise RuntimeError('BDS float wrapper is ambiguous')
        ejection_callers.add(float_matches[0]+18)
        random_float_caller=float_matches[0]+18
        locator['ejection_callers']=[hex(x) for x in sorted(ejection_callers)]
        item_matches=[rva_at(bds,p) for p in occurrences(bds.data,client.read(0x3348110,45))
                      if bds.read(rva_at(bds,p)+0x33,0x22)==client.read(0x3348143,0x22)]
        if len(item_matches)!=1:raise RuntimeError('BDS ItemActor::postNormalTick locator is ambiguous')
        item_tick_rva=item_matches[0]
        locator['item_post_tick_rva']=hex(item_tick_rva)
    if args.dropper_mixed:
        from redstone_bds_dropper_mixed_slot_probe import mixed_cases
        from redstone_bds_hopper_slot_transfer_probe import slot_script
        from redstone_bds_dispenser_switch_pulse_probe import replace_once
        cases=[c for c in mixed_cases() if c['target']=='barrel' and c['face'] in (0,2,3,4) ]
        script=held_slot_script(replace_once(slot_script(),'if(false)','if(t===60||t===76)').replace('CASES',json.dumps(cases)))
        count,frames,marker=len(cases),256,'ADAMANT_TIMING '
        # BDS allocates different registers for the outer loop: R15 is the
        # actor's Container subobject. Verify its inspected bounded-call setup.
        if bds.read(0x8A2CA40,15)!=bytes.fromhex('8d7701498b06488b40084c89f189fa'):
            raise RuntimeError('BDS outer dropper selection differs')
        container_callers[0x8A2CA55]=(0xF0,-0x110)
        for rva,size,offset in ((0x4AC4410,0x11,-0x110),):
            matches=[rva_at(bds,p) for p in occurrences(bds.data,client.read(rva,size))]
            if len(matches)!=1:raise RuntimeError('dropper bounded-call locator is ambiguous')
            container_callers[matches[0]+size+6]=(0xB0,offset)
        locator['dropper_container_callers']={hex(k):v for k,v in container_callers.items()}
    if args.piston_cross_chunk:
        from redstone_bds_piston_packet_probe import SCRIPT as PISTON_SCRIPT
        script = PISTON_SCRIPT.replace('FACINGS', '[5]').replace('PAYLOAD_COUNT', '3')
        script = script.replace(' if(t===100){',
            ' const deadline_ms=Date.now()+200;emit({checkpoint:true,tick:system.currentTick,deadline_ms});'
            'while(Date.now()<deadline_ms){}\n if(t===100){')
        count, frames, marker = 8, 100, 'ADAMANT_PISTON_PACKET '
        matches = [rva_at(bds, offset) for offset in occurrences(bds.data, client.read(0x4ADE460, 45))]
        if len(matches) != 1:raise RuntimeError('moving actor tick locator is ambiguous')
        moving_rva = matches[0]
        locator['moving_tick_rva'] = hex(moving_rva)
    if args.weather:
        script = script.replace('if(age>=1&&age<=20){const deadline_ms=',
            'if(age>=1&&age<=20){const speed=[0,1,3,0][Math.floor((age-1)/5)];'
            'd.runCommand("gamerule randomtickspeed "+speed);const deadline_ms=')
        script = script.replace('tick:system.currentTick,age,deadline_ms', 'tick:system.currentTick,age,applied_speed:speed,deadline_ms')
    if args.areas or args.circle_radius is not None:
        matches = [rva_at(bds,offset) for offset in occurrences(bds.data,client.read(0x3518090,42))]
        if len(matches)!=1:raise RuntimeError('ticking-area tick locator is ambiguous')
        area_rva=matches[0]
        locator['area_tick_rva']=hex(area_rva)
    if args.areas:
        script = script.replace('d.runCommand("tickingarea add 0 0 0 31 100 31 audit true");',
            'd.runCommand("tickingarea add -16 0 -16 31 100 31 audit_a true");'
            'd.runCommand("tickingarea add 16 0 -16 63 100 31 audit_b true");')
        script = script.replace('d.getBlock({x:0,y:65,z:0})||!d.getBlock({x:31,y:65,z:31})',
                                'd.getBlock({x:-16,y:65,z:-16})||!d.getBlock({x:63,y:65,z:31})')
        script = script.replace('ready=true;emit({ready:true',
            'd.runCommand(\'setblock 40 65 8 hopper ["facing_direction"=2]\');'
            'd.runCommand("setblock 40 65 7 barrel");d.runCommand("setblock 41 65 8 brewing_stand");'
            'ready=true;emit({ready:true')
        script = script.replace('age<=20','age<=45').replace('age===21','age===46').replace('count:20','count:45')
        frames = 45
    if args.circle_radius is not None:
        script=script.replace('d.runCommand("tickingarea add 0 0 0 31 100 31 audit true");',
            f'd.runCommand("tickingarea add circle 0 65 0 {args.circle_radius} audit true");')
        script=script.replace('!d.getBlock({x:0,y:65,z:0})||!d.getBlock({x:31,y:65,z:31})','!d.getBlock({x:0,y:65,z:0})')
        result_radius=args.circle_radius
    result = dict(reference='owned Windows BDS, hardware execution breakpoints and stable API readback',
                  complete=False, binary_sha256=hashlib.sha256(bds.data).hexdigest(),
                  client_sha256=hashlib.sha256(client.data).hexdigest(), level_seed=1,
                  locator=locator, checkpoints=[], scenarios=[])
    if args.circle_radius is not None:result['circle_radius']=result_radius
    native = NativeCoreRandom(client)
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port, script=script, local_client=True,structures=structures)
    memory, trace = None, None
    try:
        while True:
            lines = reference.until(lambda line: marker in line, timeout=90)
            record = json.loads(lines[-1].split(marker, 1)[1].strip())
            if record.get('error'):
                raise RuntimeError(record)
            if record.get('ready'):
                memory = Memory(reference.process)
                memory.snapshot()
                _, core, description = locate_level_random(memory, bds, table)
                memory.live = True
                result['owner'] = description
                if args.crafter_ejection or args.dispenser_ejection:
                    tail=memory.read(core+0x9d4,17)
                    result['initial_gaussian_cache']=dict(value=struct.unpack_from('<f',tail)[0],cached=bool(tail[16]))
                    if tail[16]:raise RuntimeError('native Gaussian stream is not initially empty')
                trace = OwnedHardwareTrace(reference.process, memory, core, core_rva, chunk_rva, native, moving_rva, area_rva,
                                           0x8A231C0 if args.crafter_ejection or args.dispenser_ejection else None,item_tick_rva,block_event_rva,bell_ring_rva)
                trace.container_callers=container_callers
                trace.ejection_callers=ejection_callers
                trace.random_float_caller=random_float_caller
                trace.start()
                reference.process.stdin.write('scriptevent audit:start\n');reference.process.stdin.flush()
            if record.get('checkpoint'):
                if time.time() * 1000 >= record['deadline_ms'] - 20:
                    raise RuntimeError('missed held trace checkpoint')
                record.update(trace.checkpoint())
                result['checkpoints'].append(record)
                if len(result['checkpoints']) % 16 == 0:
                    print(json.dumps(dict(checkpoints=len(result['checkpoints']), hits=trace.total_hits)), flush=True)
            if record.get('scenario'):
                result['scenarios'].append(record)
            if record.get('complete'):
                if len(result['scenarios']) != count or len(result['checkpoints']) != frames:
                    raise RuntimeError('incomplete actor trace')
                result['complete'] = True
                break
    finally:
        try:
            if trace:trace.stop()
        finally:
            if memory:memory.close()
            reference.stop();native.close()
            result['log_directory'] = str(reference.root)
            args.output.write_text(json.dumps(result, separators=(',', ':')) + '\n', encoding='utf-8')
            print(json.dumps(dict(complete=result['complete'], output=str(args.output), logs=str(reference.root))), flush=True)


if __name__ == '__main__':
    main()
