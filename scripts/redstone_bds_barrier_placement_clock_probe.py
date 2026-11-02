#!/usr/bin/env python3
"""Bound native placer/observer placement packets with actual Script phases."""
import json
from pathlib import Path

from redstone_bds_barrier_clock_probe import tick_marker
from redstone_bds_barrier_placement_observer_probe import capture
from redstone_bds_barrier_placement_probe import SCRIPT
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments


def clock_script():
    script = replace_once(SCRIPT, 'const h=system.runInterval(()=>{try{',
        'function clock(stage){for(const p of world.getAllPlayers())p.sendMessage("ADAMANT_CLOCK "+'
        'JSON.stringify({index,tick:system.currentTick,game_tick:world.getAbsoluteTime(),stage}));}'
        'const h=system.runInterval(()=>{try{')
    script = replace_once(script, ' current.samples.push(', ' clock("before_sample");current.samples.push(')
    script = replace_once(script, ' if(current.requested&&!current.results.length',
        ' clock("after_inputs");\n if(current.requested&&!current.results.length')
    return script


def final_marker(lines, scenes):
    target = dict(index=len(scenes)-1, tick=scenes[-1]['samples'][-1]['tick'], stage='after_inputs')
    for line in reversed(lines):
        try:
            marker = tick_marker(json.loads(line))
        except (ValueError, TypeError):
            continue
        if marker and all(marker.get(k) == v for k, v in target.items()):
            return True
    return False


def clock_packets(root, filename, scenes):
    packets, markers, sequence = [], [], 0
    for line in (root/filename).read_text(encoding='utf-8').splitlines():
        try:
            packet = json.loads(line)
        except (ValueError, TypeError):
            continue
        marker = tick_marker(packet)
        if marker:
            packet['clock'] = marker
            markers.append(marker)
        if marker or any(k in packet for k in ('block_update','subchunk_update','sound','level_event','block_event')):
            if packet['packet_sequence'] <= sequence:
                raise RuntimeError('non-monotonic native placement transcript')
            sequence = packet['packet_sequence']
            packets.append(packet)
    expected = [dict(index=i, tick=f['tick'], game_tick=f['game_tick'], stage=stage)
                for i,c in enumerate(scenes) for f in c['samples'] for stage in ('before_sample','after_inputs')]
    if markers != expected:
        raise RuntimeError('native placement phase marker stream differs or has a missing tail')
    return packets


if __name__ == '__main__':
    args = arguments()
    capture(args, source_script=clock_script(), kind='actual_player_placement_clock', drain=final_marker)
    result = json.loads(args.output.read_text(encoding='utf-8'))
    result['complete'] = False
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    root = Path(result['log_directory'])
    result['placer_clock_packets'] = clock_packets(root, 'client.log', result['scenarios'])
    result['observer_clock_packets'] = clock_packets(root, 'observer.log', result['scenarios'])
    result['complete'] = True
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, scenes=len(result['scenarios']),
        markers=sum('clock' in p for p in result['placer_clock_packets']))), flush=True)
