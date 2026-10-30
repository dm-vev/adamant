#!/usr/bin/env python3
"""Capture furnace output/fuel extraction, bucket eligibility and hopper holes."""
import json

from redstone_bds_hopper_slot_pull_probe import pull_script
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    stack = lambda name, n: dict(id='minecraft:' + name, count=n)
    cases = []
    for target in ('furnace', 'blast_furnace', 'smoker'):
        output = 'cooked_beef' if target == 'smoker' else 'iron_ingot'
        inert, coal, bucket, result = stack('diamond', 4), stack('coal', 4), stack('bucket', 1), stack(output, 4)
        full = [stack('stone', 64) for _ in range(5)]
        patterns = (
            ('output', [inert, coal, result], [None] * 5),
            ('bucket_and_output', [inert, bucket, result], [None] * 5),
            ('blocked_output', [inert, coal, result], full[:-1] + [stack(output, 63)]),
            ('earlier_hole', [inert, coal, result], [None, stack(output, 60)] + full[2:]),
            ('bucket_rejected_output_fits', [inert, bucket, result], full[:-1] + [stack(output, 60)]),
            ('input_only', [inert, coal, None], [None] * 5),
        )
        for pattern, upper, receiver in patterns:
            cases.append(dict(kind='hopper', face=5, target=target, fill=pattern, phase=0,
                              pull=True, initial_source=receiver, initial_target=upper))
    capture(args, pull_script().replace('CASES', json.dumps(cases)), len(cases))
