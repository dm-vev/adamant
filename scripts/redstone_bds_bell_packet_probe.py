#!/usr/bin/env python3
"""Capture bell redstone sound, block updates and actor packets on owned BDS."""
import hashlib
import json
from pathlib import Path

from redstone_bds_bell_pulse_probe import SCRIPT, bell_cases, readback
from redstone_bds_client import LocalClient
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


def packet_script(cases):
    script=SCRIPT.replace('CASES',json.dumps(cases))
    script=replace_once(script,'let d,loaded=0,ready=false,start=0,cells=[],serial=0;',
        'let d,loaded=0,ready=false,start=0,cells=[],serial=0,requested=false;'
        'system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")requested=true;});')
    script=replace_once(script,'  cmd("tickingarea add 0 0 0 127 100 95 audit_bell_a true");cmd("tickingarea add 0 0 96 127 100 127 audit_bell_b true");',
        '  cmd("tickingarea add -16 0 -16 47 100 47 audit_bell true");cmd("setworldspawn 16 65 12");')
    script=replace_once(script,'if(!ready){if(!get([0,65,0])||!get([127,65,127]))',
        'if(start===0){if(!get([-4,65,-4])||!get([36,65,32]))')
    script=replace_once(script,'  if(system.currentTick%20!==2)return;',
        '  if(!requested){if(!ready){ready=true;emit({ready:true});}return;}'
        'if(system.currentTick%20!==2)return;')
    return replace_once(script,'base=[8+i%8*16,65,8+Math.floor(i/8)*16]',
                        'base=[4+i%8*4,65,4+Math.floor(i/8)*4]')


if __name__=='__main__':
    args=arguments()
    cases=[c for c in bell_cases() if c['edges'] in ([0,8],[0,8,20,28])]
    if len(cases)!=32:raise RuntimeError('native bell packet controls changed')
    result=dict(complete=False,reference=__doc__,binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),
                scenarios=[],packet_observations=[])
    reference=Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=packet_script(cases),local_client=True)
    client=None
    try:
        reference.until(lambda s:'ADAMANT_TIMING ' in s and '"ready":true' in s,timeout=45)
        client=LocalClient(reference,args.port)
        reference.command('scriptevent audit:start now')
        while True:
            line=reference.until(lambda s:'ADAMANT_TIMING ' in s,timeout=45)[-1]
            record=json.loads(line.split('ADAMANT_TIMING ',1)[1])
            if record.get('error'):raise RuntimeError(record)
            if 'scenario' in record:result['scenarios'].append(record)
            if record.get('complete'):
                if record['count']!=len(cases) or len(result['scenarios'])!=len(cases):raise RuntimeError('incomplete native bell scenes')
                result['complete']=True;break
    finally:
        if client:
            client.stop()
            for line in client.log:
                try:record=json.loads(line)
                except (ValueError,TypeError):continue
                if any(k in record for k in ('block_actor','block_update','subchunk_update','block_event','sound','level_event')):
                    result['packet_observations'].append(record)
        reference.stop()
        for c in result['scenarios']:
            c['packets']=[]
            for p in result['packet_observations']:
                if 'subchunk_update' in p:
                    for b in p['subchunk_update']['Blocks']:
                        if b['BlockPos']==c['base']:
                            c['packets'].append(dict(block_update=dict(Position=b['BlockPos'],NewBlockRuntimeID=b['BlockRuntimeID'],
                                Flags=b['Flags'],Layer=0),received_at_ms=p['received_at_ms']))
                    continue
                key=next(k for k in ('block_actor','block_update','block_event','sound','level_event') if k in p)
                pos=p[key]['Position']
                if (key=='sound' and all(abs(pos[i]-c['base'][i])<=1 for i in range(3))) or (key!='sound' and pos==c['base']):
                    c['packets'].append(p)
            sounds=[p['sound'] for p in c['packets'] if 'sound' in p]
            if len(sounds)!=len(c['scenario']['edges'])//2:
                result['complete']=False;result['error']='native bell sound count control differs'
        result['log_directory']=str(reference.root)
        args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
        if result['complete']:
            result['complete']=False
            args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
            result=readback(result,Path(__file__).resolve().parents[1]/'.tmp/redstone-structure-readback.exe')
            result['complete']=True
            args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'],scenes=len(result['scenarios']),logs=str(reference.root))),flush=True)
        if not result['complete']:raise RuntimeError('native bell packet capture incomplete')
