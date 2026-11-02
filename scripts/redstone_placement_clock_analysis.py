#!/usr/bin/env python3
"""Classify native placement effects between original receiver phase markers."""
import argparse
import collections
import json
from pathlib import Path


def analyse(result):
    if not result['complete'] or result['kind'] != 'actual_player_placement_clock':
        raise RuntimeError('incomplete or wrong native placement phase corpus')
    counts, details = collections.Counter(), []
    for index, scene in enumerate(result['scenarios']):
        origin = next(f['tick'] for f in scene['samples'] if f['blocks'][0]['id'] != 'minecraft:air')
        actor, observer = scene['effect_packets'], scene['observer_effect_packets']
        private = len(actor)-len(observer)
        strip = lambda packets: [{k:v for k,v in p.items() if k not in ('packet_sequence','received_at_ms')} for p in packets]
        if strip(actor[private:]) != strip(observer):
            raise RuntimeError('native observer stream differs from public placer suffix')
        for name, effects in (('placer',actor),('observer',observer)):
            clocks = [p for p in result[name+'_clock_packets'] if 'clock' in p and p['clock']['index'] == index]
            if len(clocks) != len(scene['samples'])*2:
                raise RuntimeError('incomplete native phase clock')
            for number, packet in enumerate(effects):
                before = next((p for p in reversed(clocks) if p['packet_sequence'] < packet['packet_sequence']),None)
                after = next((p for p in clocks if p['packet_sequence'] > packet['packet_sequence']),None)
                if before is None or after is None:
                    raise RuntimeError('native effect outside retained clock bounds')
                role = 'private' if name=='placer' and number<private else 'sound' if 'sound' in packet else 'state'
                lower,upper = before['clock'],after['clock']
                key=(name,role,lower['tick']-origin,lower['stage'],upper['tick']-origin,upper['stage'])
                counts[key]+=1
                details.append(dict(scene=index,origin_tick=origin,role=role,receiver=name,
                    effect=packet,before=lower,after=upper))
    return dict(complete=True,reference='receive-order bounds around native Script interval; not an exact lower-level sender clock',
        scenes=len(result['scenarios']),effects=len(details),
        intervals=[dict(receiver=k[0],role=k[1],before_tick_offset=k[2],before_stage=k[3],
            after_tick_offset=k[4],after_stage=k[5],count=v) for k,v in sorted(counts.items())],details=details)


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--capture',type=Path,required=True)
    parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args()
    report=analyse(json.loads(args.capture.read_text(encoding='utf-8')))
    args.output.write_text(json.dumps(report,separators=(',',':'))+'\n',encoding='utf-8')
    print(json.dumps({k:v for k,v in report.items() if k!='details'}),flush=True)
