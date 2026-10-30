package world

import "math"

// The inspected Windows CRT logf uses binary32 table reduction and fused
// multiply-adds. Rounding math.Log to float32 can differ by one ULP and change
// the native Gaussian pair. These tables are derived mathematically: a coarse
// logarithm head and a positive tail rounded toward zero, plus 1/F.
var nativeGaussianLogTable = func() (table [129]struct{ inverse, head, tail float32 }) {
	for i := range table {
		value := 1 + float64(i)/128
		log := math.Log(value)
		head := math.Float32frombits(math.Float32bits(float32(log)) & 0xfffff000)
		remainder := log - float64(head)
		tail := float32(remainder)
		if float64(tail) > remainder {
			tail = math.Float32frombits(math.Float32bits(tail) - 1)
		}
		table[i].inverse, table[i].head, table[i].tail = float32(2/value), head, tail
	}
	return
}()

func nativeFMA32(a, b, c float32) float32 {
	return float32(math.FMA(float64(a), float64(b), float64(c)))
}

// nativeGaussianLog32 only receives the positive normal radius of an accepted
// Gaussian pair, below one. This is not a general-purpose logf implementation.
func nativeGaussianLog32(x float32) float32 {
	if float32(1-x) < .0625 {
		r := float32(x - 1)
		u := float32(r / float32(2+r))
		correction := float32(r * u)
		u = float32(u + u)
		u2 := float32(u * u)
		u3 := float32(u * u2)
		poly := nativeFMA32(u2, 1.0/80, 1.0/12)
		return float32(r + nativeFMA32(u3, poly, -correction))
	}
	bits := math.Float32bits(x)
	exponent := float32(int(bits>>23) - 127)
	mantissa := bits & 0x7fffff
	index := (mantissa >> 16) + ((mantissa >> 15) & 1)
	f := math.Float32frombits(index<<16 | 0x3f000000)
	y := math.Float32frombits(mantissa | 0x3f000000)
	entry := nativeGaussianLogTable[index]
	r := float32(float32(f-y) * entry.inverse)
	poly := nativeFMA32(r, 1.0/3, .5)
	r = nativeFMA32(poly, float32(r*r), r)
	ln2 := nativeGaussianLogTable[128]
	tail := nativeFMA32(exponent, ln2.tail, -r)
	head := nativeFMA32(exponent, ln2.head, entry.head)
	tail = float32(tail + entry.tail)
	return float32(head + tail)
}
