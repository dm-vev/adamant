#!/usr/bin/env python3
"""Compare recorded barrier mutations to packets between ordered Script markers."""
import argparse
import collections
import json
from pathlib import Path

from redstone_bds_client import network_state_hash


def native_hash(block):
    states = dict(block['states'])
    if 'minecraft:cardinal_direction' in states and 'direction' in states:
        expected = dict(south=0, west=1, north=2, east=3)[states['minecraft:cardinal_direction']]
        if states.pop('direction') != expected:
            raise RuntimeError('native direction aliases disagree')
    return network_state_hash(dict(id=block['id'], states=states))


def expected_effects(scene, wind=False):
    previous = scene['initial']
    result = []
    for frame in scene['samples']:
        current = frame['blocks']
        if current[0] != previous[0]:
            command = next((c for c in scene['changes'] if c.get('position') == scene['base']
                and c['tick'] == frame['tick']-1 and c['block'] == current[0]), None)
            origin = command['tick'] if command else frame['tick']
            opening = current[0]['states']['open_bit']
            door = scene['scenario']['form'] == 'door'
            if not command:
                sound_block = previous[0] if door else current[0]
                data = native_hash(sound_block)
                if data >= 0x80000000:
                    data -= 0x100000000
                sound_pos = [v+.5 for v in scene['base']]
                if door and not wind and scene['scenario']['feed'] == 'upper':
                    sound_pos[1] += 1
                name = 'fence_gate' if scene['scenario']['form'] == 'gate' else scene['scenario']['form']
                sound = dict(origin_tick=origin, cause='wind' if wind else 'circuit', role='sound',
                    expected=dict(sound=dict(SoundType=name+('.open' if opening else '.close'),
                        Position=sound_pos, ExtraData=data, EntityType='', BabyMob=False,
                        DisableRelativeVolume=False, EntityUniqueID=-1, FireAtPosition={})))
                if not wind:
                    result.append(sound)
            result.append(dict(origin_tick=origin, cause='command' if command else 'wind' if wind else 'circuit', role='lower',
                expected=dict(block_update=dict(Position=scene['base'], NewBlockRuntimeID=native_hash(current[0]), Flags=3, Layer=0))))
            if door and (not command or current[1] != previous[1]):
                result.append(dict(origin_tick=origin, cause='command' if command else 'wind' if wind else 'circuit', role='upper',
                    expected=dict(block_update=dict(Position=scene['positions'][1], NewBlockRuntimeID=native_hash(current[1]),
                        Flags=2 if command else 3, Layer=0))))
            if wind:
                result.append(sound)
        previous = current
    return result


def analyse(capture):
    if not capture['complete'] or len(capture['scenarios']) != 32:
        raise RuntimeError('incomplete native clock capture')
    transcript = capture['transcript']
    wind = capture.get('kind') == 'wind'
    previous = None
    bracketed = []
    for record in transcript:
        if 'marker' in record:
            previous = record['marker']
            continue
        bracketed.append(dict(record=record, before=previous))
    following = None
    by_sequence = {}
    for record in reversed(transcript):
        if 'marker' in record:
            following = record['marker']
        else:
            by_sequence[record['packet_sequence']] = following
    intervals = []
    totals = collections.Counter()
    for index, scene in enumerate(capture['scenarios']):
        if wind:
            # Preserve the original capture; select its two barrier cells for
            # the same field/phase comparison as the redstone control.
            scene = dict(scene, base=scene['target'], positions=scene['positions'][1:3],
                initial=scene['initial'][1:3], samples=[dict(f,blocks=f['blocks'][1:3]) for f in scene['samples']])
        actual = []
        for item in bracketed:
            packet, before = item['record'], item['before']
            if before is None or before['tick'] < scene['setup_tick']+16:
                continue
            effects = []
            if 'block_update' in packet and packet['block_update']['Position'] in scene['positions']:
                effects.append(dict(block_update=packet['block_update']))
            elif 'subchunk_update' in packet:
                for b in packet['subchunk_update']['Blocks']:
                    if b['BlockPos'] in scene['positions']:
                        effects.append(dict(block_update=dict(Position=b['BlockPos'], NewBlockRuntimeID=b['BlockRuntimeID'], Flags=b['Flags'], Layer=0)))
            elif 'sound' in packet:
                cell = [int(v//1) for v in packet['sound']['Position']]
                if cell in scene['positions']:
                    effects.append(dict(sound=packet['sound']))
            for effect in effects:
                actual.append(dict(effect=effect, before=before,
                    after=by_sequence[packet['packet_sequence']], packet_sequence=packet['packet_sequence']))
        expected = expected_effects(scene,wind=wind)
        if len(expected) != len(actual):
            raise RuntimeError(f'packet count differs for {scene["scenario"]}: expected {len(expected)}, observed {len(actual)}')
        for want, observed in zip(expected, actual):
            if want['expected'] != observed['effect'] or observed['after'] is None:
                raise RuntimeError(f'packet fields or marker bounds differ for {scene["scenario"]}: {want}, {observed}')
            origin = want['origin_tick']
            start = observed['before']['tick']-origin, observed['before']['stage']
            end = observed['after']['tick']-origin, observed['after']['stage']
            totals[want['cause'], want['role'], *start, *end] += 1
            intervals.append(dict(scene=index, **want, **observed))
    return dict(complete=True, reference='actual packet fields matched to raw native state transitions; Script marker bounds in receive order',
        intervals=intervals, counts=[dict(cause=k[0], role=k[1], before_offset=k[2], before_stage=k[3], after_offset=k[4], after_stage=k[5], count=v)
            for k, v in sorted(totals.items())])


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--capture', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    result = analyse(json.loads(args.capture.read_text(encoding='utf-8')))
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, packets=len(result['intervals']), counts=result['counts'])), flush=True)
