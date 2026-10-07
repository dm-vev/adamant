package javastructure

import (
	"math"
)

func transformEntityNBT(source map[string]any, mirror Mirror, rotation Rotation, position [3]float64, blockPos [3]int32) map[string]any {
	if len(source) == 0 { return source }
	out := make(map[string]any, len(source)+1)
	for key, value := range source { out[key] = value }
	delete(out, "UUID")
	delete(out, "UUIDMost")
	delete(out, "UUIDLeast")
	out["Pos"] = []any{position[0],position[1],position[2]}
	if raw, ok := source["block_pos"].(map[string]any); ok {
		pos := make(map[string]any,len(raw)+3)
		for key, value := range raw { pos[key] = value }
		pos["X"], pos["Y"], pos["Z"] = blockPos[0],blockPos[1],blockPos[2]
		out["block_pos"] = pos
	}
	if raw, ok := source["Rotation"].([]any); ok && len(raw) >= 1 {
		if yaw, ok := number(raw[0]); ok {
			x, z := -math.Sin(yaw*math.Pi/180), math.Cos(yaw*math.Pi/180)
			v := transformVector(x,z,mirror,rotation)
			newYaw := math.Atan2(-v[0],v[1])*180/math.Pi
			if len(raw) == 2 { values := []any{raw[0],raw[1]}; values[0] = convertedFloat(raw[0],newYaw); out["Rotation"] = values } else { values := make([]any,len(raw)); copy(values,raw); values[0] = convertedFloat(raw[0],newYaw); out["Rotation"] = values }
		}
	}
	if facing, ok := integer(source["Facing"]); ok {
		directions := [...]string{"", "", "north", "south", "west", "east"}
		if facing >= 2 && facing <= 5 {
			d := transformEntityDirection(directions[facing],mirror,rotation)
			for i:=int64(2); i<=5; i++ { if directions[i] == d { out["Facing"] = convertedInteger(source["Facing"],i); break } }
		}
	}
	return out
}

func number(v any) (float64,bool) { switch n:=v.(type) { case float32: return float64(n),true; case float64: return n,true; default: return 0,false } }
func convertedFloat(original any, v float64) any { if _,ok:=original.(float32); ok { return float32(v) }; return v }
func convertedInteger(original any,v int64) any { switch original.(type) { case int8:return int8(v);case int16:return int16(v);case int32:return int32(v);case int:return int(v);default:return v } }
func transformEntityDirection(direction string, mirror Mirror, rotation Rotation) string {
	var x,z float64
	switch direction { case "north": z = -1; case "south": z = 1; case "west": x = -1; case "east": x = 1; default: return direction }
	v := transformVector(x,z,mirror,rotation)
	switch { case v[0] > 0: return "east"; case v[0] < 0: return "west"; case v[1] > 0: return "south"; default: return "north" }
}
