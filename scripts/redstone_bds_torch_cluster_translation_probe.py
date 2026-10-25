#!/usr/bin/env python3
"""Translate the exact four-torch fixture across positions/chunk boundaries."""

from redstone_bds_timing_probe import arguments, capture
from redstone_bds_torch_cluster_probe import SCRIPT as BASE


def replace_once(script, old, new):
    if script.count(old) != 1:
        raise ValueError("cluster geometry template changed")
    return script.replace(old, new, 1)


SCRIPT = replace_once(BASE,
    'for(const sides of [["east","west"],["east","north"],["east","north","west"],["east","north","west","south"]])for(const reverse of [false,true])for(const phase of [0,1])cases.push({sides,reverse,phase});',
    'for(const center of [[0,64,0],[8,64,8],[16,64,16],[-1,64,-1],[31,70,15]])for(const reverse of [false,true])for(const phase of [0,1])cases.push({sides:["east","north","west","south"],center,reverse,phase});')
SCRIPT = replace_once(SCRIPT, 'const cmd=x=>dimension.runCommand(x);', r'''
function offset(p){const c=cases[index].center;return [p[0]+c[0],p[1]+c[1]-64,p[2]+c[2]];}
function cmd(x){
    const parts=x.split(" ");
    if(parts[0]==="setblock"||parts[0]==="fill"){
        const groups=parts[0]==="fill"?[1,4]:[1];
        for(const i of groups){const p=offset(parts.slice(i,i+3).map(Number));for(let j=0;j<3;j++)parts[i+j]=String(p[j]);}
        x=parts.join(" ");
    }
    return dimension.runCommand(x);
}
''')
SCRIPT = replace_once(SCRIPT, 'const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});',
                      'p=offset(p);const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});')
SCRIPT = replace_once(SCRIPT, 'pos:[0,63,0],powered', 'pos:offset([0,63,0]),powered')
SCRIPT = replace_once(SCRIPT, 'scenario:c,positions,samples,changes', 'scenario:c,positions:positions.map(offset),samples,changes')
SCRIPT = replace_once(SCRIPT, 't>=112', 't>=80')
SCRIPT = replace_once(SCRIPT, 'tickingarea add -16 0 -16 16 100 16', 'tickingarea add -16 0 -16 48 100 32')


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 20)
