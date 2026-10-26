"""Encode authored one-block audit structures, without game assets or dependencies."""

import struct


def byte(value):
    return 1, value


def integer(value):
    return 3, value


def string(value):
    return 8, value


def listing(kind, values):
    return 9, (kind, values)


def compound(value):
    return 10, value


def _text(value):
    encoded = value.encode("utf-8")
    return struct.pack("<H", len(encoded)) + encoded


def _payload(kind, value):
    if kind == 1:
        return struct.pack("<b", value)
    if kind == 2:
        return struct.pack("<h", value)
    if kind == 3:
        return struct.pack("<i", value)
    if kind == 5:
        return struct.pack("<f", value)
    if kind == 8:
        return _text(value)
    if kind == 9:
        element, values = value
        return struct.pack("<bi", element, len(values)) + b"".join(_payload(element, v) for v in values)
    if kind == 10:
        return b"".join(bytes([t]) + _text(n) + _payload(t, v) for n, (t, v) in value.items()) + b"\0"
    raise ValueError("unsupported authored NBT type")


def one_block(name, states, actor):
    """Create a little-endian MCStructure with an explicitly supplied block actor.

    This is a persistence-state input, not a simulated player interaction.
    The native BDS loader and world ticks interpret it during capture.
    """
    root = {
        "format_version": integer(1),
        "size": listing(3, [1, 1, 1]),
        "structure_world_origin": listing(3, [0, 0, 0]),
        "structure": compound({
            "block_indices": listing(9, [(3, [0]), (3, [-1])]),
            "entities": listing(10, []),
            "palette": compound({"default": compound({
                "block_palette": listing(10, [{"name": string(name), "states": compound(states),
                                               "version": integer(18168865)}]),
                "block_position_data": compound({"0": compound({"block_entity_data": compound({
                    "x": integer(0), "y": integer(0), "z": integer(0), **actor,
                })})}),
            })}),
        }),
    }
    return b"\x0a\0\0" + _payload(10, root)
