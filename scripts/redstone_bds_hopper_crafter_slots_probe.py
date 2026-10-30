#!/usr/bin/env python3
"""Capture all-sided hopper insertion into enabled crafter slots.

These scenes isolate slot balancing and rejected source stacks. They do not
power the crafter or claim recipe/crafting-clock/disabled-slot equivalence.
"""
import json
from pathlib import Path
import subprocess

from redstone_audit_structure import byte, string, listing, one_block
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_hopper_slot_transfer_probe import slot_script
from redstone_bds_timing_probe import arguments, capture


def crafter_script(pull=False):
    from redstone_bds_hopper_slot_pull_probe import pull_script
    script=replace_once(pull_script() if pull else slot_script(),'set(target,scenario.target);',
        'set(target,scenario.target+\' ["orientation"="north_up"]\');'
        'if(scenario.target_empty_structure&&!cmd(`structure load ${scenario.target_empty_structure.replace("/",":")} ${target.join(" ")}`).successCount)throw new Error("crafter empty-slot control failed");')
    script=replace_once(script,'const slots=p=>{const c=inv(p),out=[];',
        'let snapshotSerial=0;const snapshot=p=>{const name="audit_crafter_snap_"+snapshotSerial++;'
        'if(!cmd(`structure save ${name} ${p.join(" ")} ${p.join(" ")} false disk true`).successCount)throw new Error("snapshot save failed");'
        'return {snapshot:"mystructure:"+name};};'
        'const slots=p=>{if(block(p).typeId==="minecraft:crafter")return snapshot(p);const c=inv(p),out=[];')
    script=replace_once(script,
        'const inventory=inv(pos);if(items.length!==inventory.size)throw new Error("initial slot count mismatch");\n'
        '    for(let i=0;i<items.length;i++){const s=items[i];if(s)inventory.setItem(i,new ItemStack(s.id,s.count));}',
        'if(block(pos).typeId==="minecraft:crafter"){'
        'if(!cmd(`structure load ${c.scenario.target_structure.replace("/",":")} ${pos.join(" ")}`).successCount)throw new Error("crafter input structure failed");'
        '}else{const inventory=inv(pos);if(items.length!==inventory.size)throw new Error("initial slot count mismatch");'
        'for(let i=0;i<items.length;i++){const s=items[i];if(s)inventory.setItem(i,new ItemStack(s.id,s.count));}}')
    return script


def readback(result,helper):
    database=Path(result['log_directory'])/'worlds/redstone-reference/db'
    completed=subprocess.run([str(helper),'-db',str(database),'-prefix','structuretemplate_mystructure:audit_crafter_snap_','-immutable-audit-wal'],
                             capture_output=True,text=True,encoding='utf-8',check=True)
    snapshots={}
    for line in completed.stdout.splitlines():
        record=json.loads(line)
        root=record['structure']
        if root['format_version']!=2 or root['size']!=[1,1,1]:raise RuntimeError('invalid native snapshot size/version')
        palette=root['structure']['palette']['default']
        block=palette['block_palette'][0]
        if block['name']!='minecraft:crafter' or block['states']!={'orientation':'north_up','crafting':0,'triggered_bit':0}:
            raise RuntimeError('native crafter snapshot permutation differs')
        if record['state_types']!=[{'orientation':'string','crafting':'uint8','triggered_bit':'uint8'}]:
            raise RuntimeError('native crafter palette state types differ')
        actor=palette['block_position_data']['0']['block_entity_data']
        if actor['id']!='Crafter' or not 0<=actor['disabled_slots']<=511:raise RuntimeError('native crafter actor/slot-mask control differs')
        slots=[None]*9
        for stack in actor['Items']:
            slot=stack['Slot']
            if not 0<=slot<9 or slots[slot] is not None:raise RuntimeError('invalid native crafter slot index')
            if stack.get('Count',0):slots[slot]=dict(id=stack['Name'],count=stack['Count'])
        if record['name'] in snapshots:raise RuntimeError('duplicate snapshot name')
        snapshots[record['name']]=dict(slots=slots,disabled=actor['disabled_slots'])
    for case in result['scenarios']:
        if len(case['samples'])!=128:raise RuntimeError('invalid native crafter sample count')
        for ref in [case['load_control']['target']]+[frame['target_items'] for frame in case['samples']]:
            saved=snapshots.get(ref['snapshot'])
            if saved is None or saved['disabled']!=case['scenario'].get('disabled_mask',0):raise RuntimeError('missing native snapshot or disabled-slot control differs')
    used=set()

    def resolve(value):
        if isinstance(value,dict):
            if set(value)=={'snapshot'}:
                name=value['snapshot']
                if name not in snapshots or name in used:raise RuntimeError('missing/reused native snapshot')
                used.add(name)
                return snapshots[name]['slots']
            return {k:resolve(v) for k,v in value.items()}
        if isinstance(value,list):return [resolve(v) for v in value]
        return value

    resolved=resolve(result)
    if len(used)!=len(result['scenarios'])*129 or len(snapshots)!=len(used):raise RuntimeError(f'incomplete crafter NBT readback: {len(used)}/{len(snapshots)}')
    for case in resolved['scenarios']:
        if case['load_control']!={'source':case['scenario']['initial_source'],'target':case['scenario']['initial_target']}:
            raise RuntimeError('native initial crafter/hopper slot readback differs')
    resolved['target_inventory_readback']=dict(method='native per-tick structure snapshots, stopped BDS LevelDB opened read-only',
                                               snapshots=len(used),key_prefix='structuretemplate_mystructure:audit_crafter_snap_',
                                               verified_journal_readback=completed.stderr.strip())
    return resolved


def crafter_structure(items,disabled=0):
    stacks=[{'Slot':byte(i),'Name':string(s['id']),'Count':byte(s['count']),'Damage':(2,0)} for i,s in enumerate(items) if s]
    return one_block('minecraft:crafter',{'orientation':string('north_up'),'crafting':byte(0),'triggered_bit':byte(0)},
                     {'id':string('Crafter'),'disabled_slots':(2,disabled),'Items':listing(10,stacks)})


def capture_crafter_slots(args,cases,structures,pull=False):
    root=Path(__file__).resolve().parents[1]
    helper=root/f'.tmp/redstone-structure-readback-{args.port}.exe'
    helper.parent.mkdir(exist_ok=True)
    subprocess.run(['go','build','-o',str(helper),'./cmd/redstone-structure-readback'],cwd=root,check=True)
    destination=args.output
    args.output=root/f'.tmp/crafter_slots_script_raw_{args.port}.json'
    capture(args,crafter_script(pull).replace('CASES',json.dumps(cases)),len(cases),structures=structures)
    result=readback(json.loads(args.output.read_text(encoding='utf-8')),helper)
    destination.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
    print(json.dumps(dict(complete=True,scenarios=len(cases),snapshots=len(cases)*129,output=str(destination))),flush=True)


if __name__=='__main__':
    args=arguments()
    args.offline_reference=True
    stack=lambda name,n:dict(id='minecraft:'+name,count=n)
    diamond=lambda n:stack('diamond',n)
    dirt=lambda n:stack('dirt',n)
    stone=stack('stone',64)
    ordinary=[diamond(4),dirt(4),None,None,None]
    patterns=(
        ('empty',ordinary,[None]*9),
        ('later_empty',ordinary,[diamond(60),None]+[dirt(64)]*7),
        ('later_smaller_matching',ordinary,[diamond(4),diamond(1)]+[dirt(64)]*7),
        ('earlier_smaller_matching',ordinary,[diamond(1),diamond(4)]+[dirt(64)]*7),
        ('different_items',ordinary,[dirt(1),diamond(5),None]+[dirt(64)]*6),
        ('rejected_first_source',ordinary,[stone]*8+[dirt(60)]),
        ('full',ordinary,[stone]*9),
        ('stack_limit',ordinary,[diamond(64)]*8+[diamond(63)]),
        ('equal_counts',ordinary,[diamond(1)]*9),
        ('holes_after_incompatible',ordinary,[dirt(64),diamond(8),None,diamond(2)]+[stone]*5),
        ('nonstackable',[stack('iron_sword',1),diamond(4),None,None,None],
         [stack('iron_sword',1),None]+[dirt(64)]*7),
        ('mixed_maxima',[stack('glass_bottle',4),diamond(4),None,None,None],
         [stack('glass_bottle',60),stack('glass_bottle',1)]+[stone]*7),
    )
    cases=[dict(kind='hopper',face=face,target='crafter',fill=pattern,phase=0,
                initial_source=source,initial_target=target,target_structure=f'audit/crafter_slots_{pattern}',target_orientation='north_up')
           for face in (0,2,3,4,5) for pattern,source,target in patterns]
    structures={}
    for pattern,_,target in patterns:
        structures[f'audit/crafter_slots_{pattern}']=crafter_structure(target)
    capture_crafter_slots(args,cases,structures)
