package genlayer

import "sync"

var intsPool sync.Pool

func borrowInts(n int) []int {
	if v := intsPool.Get(); v != nil {
		buf := v.([]int)
		if cap(buf) >= n {
			return buf[:n]
		}
		// Keep smaller buffers available for later small requests.
		intsPool.Put(buf[:0])
	}
	return make([]int, n)
}

func releaseInts(buf []int) {
	if buf == nil {
		return
	}
	intsPool.Put(buf[:0])
}

// ReleaseInts releases a temporary biome layer buffer previously returned by a layer call.
func ReleaseInts(buf []int) {
	releaseInts(buf)
}
