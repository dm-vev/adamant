#!/usr/bin/env python3
"""Check native cart birth ticks across negative chunks and the coordinate origin."""
from copy import copy
import json

from redstone_bds_dispenser_cart_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    parts = []
    for faces in ((2, 3), (4, 5)):
        cases = [dict(item=kind,face=face,slope=True,anchor=anchor,phase=0)
                 for kind in ('minecraft:minecart','minecraft:chest_minecart')
                 for face in faces for anchor in (-17,-16,-1,0,15,16)]
        old_base = 'base=[64+i%2*128,65,64+Math.floor(i/2)*128]'
        new_base = 'base=scenario.face<=3?[64+i*48,65,scenario.anchor]:[scenario.anchor,65,64+i*48]'
        script = SCRIPT.replace(old_base, new_base)
        old_force = 'for(let z=0;z<512;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${Math.min(z+95,511)} audit_${z} true`);'
        if faces[0] == 2:
            new_force = 'for(let x=0;x<512;x+=96)cmd(`tickingarea add ${x} 0 -64 ${Math.min(x+95,511)} 100 191 audit_${x} true`);'
            loaded = 'for(let x=0;x<512;x+=16)for(let z=-64;z<192;z+=16)'
        else:
            new_force = 'for(let z=0;z<512;z+=96)cmd(`tickingarea add -64 0 ${z} 191 100 ${Math.min(z+95,511)} audit_${z} true`);'
            loaded = 'for(let x=-64;x<192;x+=16)for(let z=0;z<512;z+=16)'
        if old_base not in SCRIPT or old_force not in script:
            raise ValueError('native dispenser control domain changed')
        script = script.replace(old_force, new_force)
        script = script.replace('for(let x=0;x<256;x+=16)for(let z=0;z<512;z+=16)', loaded)
        part = copy(args)
        part.output = args.output.with_name(args.output.stem + f'-faces{faces[0]}' + args.output.suffix)
        capture(part, script.replace('CASES', json.dumps(cases)), len(cases))
        parts.append(json.loads(part.output.read_text()))
    result = dict(parts[0])
    result['scenarios'] = [c for p in parts for c in p['scenarios']]
    result['log_directories'] = [p['log_directory'] for p in parts]
    result.pop('log_directory', None)
    if len(result['scenarios']) != 48 or not all(p['complete'] for p in parts):
        raise ValueError('incomplete signed chunk capture')
    args.output.write_text(json.dumps(result), encoding='utf-8')
    print(json.dumps(dict(complete=True,count=48,output=str(args.output))),flush=True)
