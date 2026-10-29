#!/usr/bin/env python3
"""Capture actual note block events and nearby sound/particle packets from BDS."""
import hashlib
import json

from redstone_bds_client import LocalClient
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_note_pulse_probe import SCRIPT, note_cases, note_structures
from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


def packet_script(cases):
    script = SCRIPT.replace('CASES',json.dumps(cases))
    script = replace_once(script,'let d,loaded=0,ready=false,start=0,cells=[];',
        'let d,loaded=0,ready=false,start=0,cells=[],requested=false;'
        'system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")requested=true;});')
    script = replace_once(script,'  for(let z=0;z<192;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${z+95} audit_note_${z} true`);',
        '  cmd("tickingarea add -16 0 -16 47 100 47 audit_note true");cmd("setworldspawn 16 65 12");')
    script = replace_once(script,'if(!ready){if(!get([0,65,0])||!get([255,65,191]))',
        'if(start===0){if(!get([-4,65,-4])||!get([36,65,32]))')
    script = replace_once(script,'  if(system.currentTick%20!==2)return;',
        '  if(!requested){if(!ready){ready=true;emit({ready:true});}return;}'
        'if(system.currentTick%20!==2)return;')
    return replace_once(script,'base=[8+i%12*20,65,8+Math.floor(i/12)*20]',
                        'base=[4+i%8*4,65,4+Math.floor(i/8)*4]')


if __name__ == '__main__':
    args = arguments()
    cases = [c for c in note_cases() if c['edges'] in ([0,8],[0,8,20,28]) and c['above']!='water']
    if len(cases)!=46:
        raise RuntimeError('note packet scene controls changed')
    result = dict(complete=False, reference=__doc__, binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),
                  scenarios=[], packet_observations=[])
    reference = Reference(args.binary.resolve(),args.assets.resolve(),args.port,
                          script=packet_script(cases),structures=note_structures(),local_client=True)
    client = None
    try:
        reference.until(lambda s:'ADAMANT_TIMING ' in s and '"ready":true' in s,timeout=45)
        client=LocalClient(reference,args.port)
        reference.command('scriptevent audit:start now')
        while True:
            line=reference.until(lambda s:'ADAMANT_TIMING ' in s,timeout=30)[-1]
            record=json.loads(line.split('ADAMANT_TIMING ',1)[1])
            if record.get('error'):
                raise RuntimeError(record)
            if 'scenario' in record:
                result['scenarios'].append(record)
            if record.get('complete'):
                if record['count']!=len(cases) or len(result['scenarios'])!=len(cases):
                    raise RuntimeError('incomplete native note packet scenes')
                result['complete']=True
                break
    finally:
        if client:
            client.stop()
            for line in client.log:
                try:
                    record=json.loads(line)
                except (ValueError,TypeError):
                    continue
                if any(key in record for key in ('block_event','sound','level_event')):
                    result['packet_observations'].append(record)
        reference.stop()
        for c in result['scenarios']:
            c['packets']=[]
            for p in result['packet_observations']:
                value=p.get('block_event') or p.get('sound') or p['level_event']
                if all(abs(value['Position'][i]-c['base'][i])<=1.5 for i in range(3)):
                    c['packets'].append(p)
            events=[p['block_event'] for p in c['packets'] if 'block_event' in p]
            count=len(c['scenario']['edges'])//2 if c['scenario']['above']=='air' else 0
            if len(events)!=count or any(e['EventData']!=c['scenario']['pitch'] or not 0<=e['EventType']<=15 for e in events):
                result['complete']=False
                result['error']='native note block event/pitch controls differ'
        result['log_directory']=str(reference.root)
        args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'],scenes=len(result['scenarios']),
                             packets=sum(len(c['packets']) for c in result['scenarios']),logs=str(reference.root))),flush=True)
        if not result['complete']:
            raise RuntimeError('native note packet capture incomplete')
