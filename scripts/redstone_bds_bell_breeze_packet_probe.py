#!/usr/bin/env python3
"""Capture bell packets from redirected real breeze-fired native charges."""
from redstone_bds_bell_wind_packet_probe import capture_bell_wind_packets
from redstone_bds_timing_probe import arguments


if __name__ == '__main__':
    capture_bell_wind_packets(arguments(), breeze=True)
