package overworld

// The pinned x86-64 JVM uses a pi/32 reduction and compensated polynomial
// summation for Math.sin/cos. Go's math.Cos differs by an ulp at some ellipse
// boundaries, changing voxel eligibility and the following random stream.
// This independently evaluates that numerical scheme for the feature's
// angles [0, 5*pi/2]; it is not a general-purpose trigonometric API.
// Algorithm reference:
// https://github.com/openjdk/jdk/blob/jdk-25-ga/src/hotspot/cpu/x86/stubGenerator_x86_64_sin.cpp
// Lookup data below are independently derived mathematical values, not a
// copied JVM table: cos(B)-sigma, sin(B), float32(sin(B)-double(sin(B))), sigma,
// where B=n*pi/32 and sigma is the closest signed power of two to cos(B).
// Explicit float64 conversions preserve the separate JVM roundings, forbidding FMA.
func icebergSinCos(angle float64) (sin, cos float64) {
	const inverse = 32 / 3.1415926535897932384626433832795028841971693993751
	const p1 = 0x1.921fb54400000p-4
	const p2 = 0x1.0b4611a600000p-38
	const p3 = 0x1.3198a2e037073p-73
	scaled := float64(angle * inverse)
	n := int(float64(scaled + .5))
	factor := float64(n)
	m1 := float64(factor * p1)
	m2 := float64(factor * p2)
	r1 := float64(angle - m1)
	r := float64(r1 - m2)
	c1 := float64(r1 - r)
	c2 := float64(c1 - m2)
	m3 := float64(factor * p3)
	negativeTail := float64(m3 - c2)
	return icebergTrigReduced(n, r, r1, negativeTail), icebergTrigReduced(n+16, r, r1, negativeTail)
}

func icebergTrigReduced(index int, r, r1, negativeTail float64) float64 {
	t := &icebergTrigTable[index&63]
	cosine := float64(t[0] + t[3])
	r2 := float64(r * r)
	r4 := float64(r2 * r2)
	sine4 := float64((1.0 / 362880) * r1)
	sine4 = float64(sine4 * r)
	cosine4 := float64((1.0 / 40320) * r1)
	cosine4 = float64(cosine4 * r)
	sine3 := float64(-1.0/5040 + sine4)
	cosine3 := float64(-1.0/720 + cosine4)
	sine2 := float64((1.0 / 120) * r2)
	cosine2 := float64((1.0 / 24) * r2)
	sine1 := float64(-1.0/6 + sine2)
	cosine1 := float64(-.5 + cosine2)
	sine3 = float64(sine3 * r4)
	cosine3 = float64(cosine3 * r4)
	sinePolynomial := float64(sine1 + sine3)
	cosinePolynomial := float64(cosine1 + cosine3)
	sineFactor := float64(cosine * r)
	sineFactor = float64(sineFactor * r2)
	cosineFactor := float64(t[1] * r2)
	sinePolynomial = float64(sinePolynomial * sineFactor)
	cosinePolynomial = float64(cosinePolynomial * cosineFactor)
	derivative := float64(t[1] * r)
	derivative = float64(derivative - cosine)
	correction := float64(negativeTail * derivative)
	correction = float64(correction + t[2])
	sigmaR := float64(t[3] * r)
	intermediate := float64(sigmaR + t[1])
	medium := float64(t[0] * r)
	high := float64(medium + intermediate)
	low1 := float64(t[1] - intermediate)
	low1 = float64(low1 + sigmaR)
	low2 := float64(intermediate - high)
	low2 = float64(low2 + medium)
	low := float64(low1 + correction)
	low = float64(low + low2)
	low = float64(low + sinePolynomial)
	low = float64(low + cosinePolynomial)
	return float64(low + high)
}

var icebergTrigTable = [64][4]float64{
	{0x0.0p+0, 0x0.0p+0, 0x0.0p+0, 0x1.0000000000000p+0},
	{-0x1.3b92e176d6d31p-8, 0x1.917a6bc29b42cp-4, -0x1.e2718e0000000p-60, 0x1.0000000000000p+0},
	{-0x1.3ad06011469fbp-6, 0x1.8f8b83c69a60bp-3, -0x1.26d19c0000000p-57, 0x1.0000000000000p+0},
	{-0x1.60bea939d225ap-5, 0x1.294062ed59f06p-2, -0x1.5d28da0000000p-56, 0x1.0000000000000p+0},
	{-0x1.37ca1866b95cfp-4, 0x1.87de2a6aea963p-2, -0x1.72cede0000000p-57, 0x1.0000000000000p+0},
	{-0x1.e3a6873fa1279p-4, 0x1.e2b5d3806f63bp-2, 0x1.e0d8920000000p-58, 0x1.0000000000000p+0},
	{-0x1.592675bc57974p-3, 0x1.1c73b39ae68c8p-1, 0x1.b25dd20000000p-55, 0x1.0000000000000p+0},
	{-0x1.d0dfe53aba2fdp-3, 0x1.44cf325091dd6p-1, 0x1.8076a20000000p-57, 0x1.0000000000000p+0},
	{0x1.a827999fcef32p-3, 0x1.6a09e667f3bcdp-1, -0x1.bdd3420000000p-55, 0x1.0000000000000p-1},
	{0x1.133cc94247758p-3, 0x1.8bc806b151741p-1, -0x1.2c5e120000000p-55, 0x1.0000000000000p-1},
	{0x1.c73b39ae68c87p-5, 0x1.a9b66290ea1a3p-1, 0x1.9f630e0000000p-60, 0x1.0000000000000p-1},
	{-0x1.d4a2c7f909c4ep-6, 0x1.c38b2f180bdb1p-1, -0x1.6e0b180000000p-56, 0x1.0000000000000p-1},
	{-0x1.e087565455a75p-4, 0x1.d906bcf328d46p-1, 0x1.457e620000000p-56, 0x1.0000000000000p-1},
	{0x1.4a03176acf82dp-5, 0x1.e9f4156c62ddap-1, 0x1.760b1e0000000p-55, 0x1.0000000000000p-2},
	{-0x1.c1d1f0e5967d5p-5, 0x1.f6297cff75cb0p-1, 0x1.5621720000000p-56, 0x1.0000000000000p-2},
	{-0x1.ba1650f592f50p-6, 0x1.fd88da3d12526p-1, -0x1.87df640000000p-55, 0x1.0000000000000p-3},
	{0x0.0p+0, 0x1.0000000000000p+0, 0x0.0p+0, 0x0.0p+0},
	{0x1.ba1650f592f50p-6, 0x1.fd88da3d12526p-1, -0x1.87df640000000p-55, -0x1.0000000000000p-3},
	{0x1.c1d1f0e5967d5p-5, 0x1.f6297cff75cb0p-1, 0x1.5621720000000p-56, -0x1.0000000000000p-2},
	{-0x1.4a03176acf82dp-5, 0x1.e9f4156c62ddap-1, 0x1.760b1e0000000p-55, -0x1.0000000000000p-2},
	{0x1.e087565455a75p-4, 0x1.d906bcf328d46p-1, 0x1.457e620000000p-56, -0x1.0000000000000p-1},
	{0x1.d4a2c7f909c4ep-6, 0x1.c38b2f180bdb1p-1, -0x1.6e0b180000000p-56, -0x1.0000000000000p-1},
	{-0x1.c73b39ae68c87p-5, 0x1.a9b66290ea1a3p-1, 0x1.9f630e0000000p-60, -0x1.0000000000000p-1},
	{-0x1.133cc94247758p-3, 0x1.8bc806b151741p-1, -0x1.2c5e120000000p-55, -0x1.0000000000000p-1},
	{-0x1.a827999fcef32p-3, 0x1.6a09e667f3bcdp-1, -0x1.bdd3420000000p-55, -0x1.0000000000000p-1},
	{0x1.d0dfe53aba2fdp-3, 0x1.44cf325091dd6p-1, 0x1.8076a20000000p-57, -0x1.0000000000000p+0},
	{0x1.592675bc57974p-3, 0x1.1c73b39ae68c8p-1, 0x1.b25dd20000000p-55, -0x1.0000000000000p+0},
	{0x1.e3a6873fa1279p-4, 0x1.e2b5d3806f63bp-2, 0x1.e0d8920000000p-58, -0x1.0000000000000p+0},
	{0x1.37ca1866b95cfp-4, 0x1.87de2a6aea963p-2, -0x1.72cede0000000p-57, -0x1.0000000000000p+0},
	{0x1.60bea939d225ap-5, 0x1.294062ed59f06p-2, -0x1.5d28da0000000p-56, -0x1.0000000000000p+0},
	{0x1.3ad06011469fbp-6, 0x1.8f8b83c69a60bp-3, -0x1.26d19c0000000p-57, -0x1.0000000000000p+0},
	{0x1.3b92e176d6d31p-8, 0x1.917a6bc29b42cp-4, -0x1.e2718e0000000p-60, -0x1.0000000000000p+0},
	{0x0.0p+0, 0x0.0p+0, 0x0.0p+0, -0x1.0000000000000p+0},
	{0x1.3b92e176d6d31p-8, -0x1.917a6bc29b42cp-4, 0x1.e2718e0000000p-60, -0x1.0000000000000p+0},
	{0x1.3ad06011469fbp-6, -0x1.8f8b83c69a60bp-3, 0x1.26d19c0000000p-57, -0x1.0000000000000p+0},
	{0x1.60bea939d225ap-5, -0x1.294062ed59f06p-2, 0x1.5d28da0000000p-56, -0x1.0000000000000p+0},
	{0x1.37ca1866b95cfp-4, -0x1.87de2a6aea963p-2, 0x1.72cede0000000p-57, -0x1.0000000000000p+0},
	{0x1.e3a6873fa1279p-4, -0x1.e2b5d3806f63bp-2, -0x1.e0d8920000000p-58, -0x1.0000000000000p+0},
	{0x1.592675bc57974p-3, -0x1.1c73b39ae68c8p-1, -0x1.b25dd20000000p-55, -0x1.0000000000000p+0},
	{0x1.d0dfe53aba2fdp-3, -0x1.44cf325091dd6p-1, -0x1.8076a20000000p-57, -0x1.0000000000000p+0},
	{-0x1.a827999fcef32p-3, -0x1.6a09e667f3bcdp-1, 0x1.bdd3420000000p-55, -0x1.0000000000000p-1},
	{-0x1.133cc94247758p-3, -0x1.8bc806b151741p-1, 0x1.2c5e120000000p-55, -0x1.0000000000000p-1},
	{-0x1.c73b39ae68c87p-5, -0x1.a9b66290ea1a3p-1, -0x1.9f630e0000000p-60, -0x1.0000000000000p-1},
	{0x1.d4a2c7f909c4ep-6, -0x1.c38b2f180bdb1p-1, 0x1.6e0b180000000p-56, -0x1.0000000000000p-1},
	{0x1.e087565455a75p-4, -0x1.d906bcf328d46p-1, -0x1.457e620000000p-56, -0x1.0000000000000p-1},
	{-0x1.4a03176acf82dp-5, -0x1.e9f4156c62ddap-1, -0x1.760b1e0000000p-55, -0x1.0000000000000p-2},
	{0x1.c1d1f0e5967d5p-5, -0x1.f6297cff75cb0p-1, -0x1.5621720000000p-56, -0x1.0000000000000p-2},
	{0x1.ba1650f592f50p-6, -0x1.fd88da3d12526p-1, 0x1.87df640000000p-55, -0x1.0000000000000p-3},
	{0x0.0p+0, -0x1.0000000000000p+0, 0x0.0p+0, 0x0.0p+0},
	{-0x1.ba1650f592f50p-6, -0x1.fd88da3d12526p-1, 0x1.87df640000000p-55, 0x1.0000000000000p-3},
	{-0x1.c1d1f0e5967d5p-5, -0x1.f6297cff75cb0p-1, -0x1.5621720000000p-56, 0x1.0000000000000p-2},
	{0x1.4a03176acf82dp-5, -0x1.e9f4156c62ddap-1, -0x1.760b1e0000000p-55, 0x1.0000000000000p-2},
	{-0x1.e087565455a75p-4, -0x1.d906bcf328d46p-1, -0x1.457e620000000p-56, 0x1.0000000000000p-1},
	{-0x1.d4a2c7f909c4ep-6, -0x1.c38b2f180bdb1p-1, 0x1.6e0b180000000p-56, 0x1.0000000000000p-1},
	{0x1.c73b39ae68c87p-5, -0x1.a9b66290ea1a3p-1, -0x1.9f630e0000000p-60, 0x1.0000000000000p-1},
	{0x1.133cc94247758p-3, -0x1.8bc806b151741p-1, 0x1.2c5e120000000p-55, 0x1.0000000000000p-1},
	{0x1.a827999fcef32p-3, -0x1.6a09e667f3bcdp-1, 0x1.bdd3420000000p-55, 0x1.0000000000000p-1},
	{-0x1.d0dfe53aba2fdp-3, -0x1.44cf325091dd6p-1, -0x1.8076a20000000p-57, 0x1.0000000000000p+0},
	{-0x1.592675bc57974p-3, -0x1.1c73b39ae68c8p-1, -0x1.b25dd20000000p-55, 0x1.0000000000000p+0},
	{-0x1.e3a6873fa1279p-4, -0x1.e2b5d3806f63bp-2, -0x1.e0d8920000000p-58, 0x1.0000000000000p+0},
	{-0x1.37ca1866b95cfp-4, -0x1.87de2a6aea963p-2, 0x1.72cede0000000p-57, 0x1.0000000000000p+0},
	{-0x1.60bea939d225ap-5, -0x1.294062ed59f06p-2, 0x1.5d28da0000000p-56, 0x1.0000000000000p+0},
	{-0x1.3ad06011469fbp-6, -0x1.8f8b83c69a60bp-3, 0x1.26d19c0000000p-57, 0x1.0000000000000p+0},
	{-0x1.3b92e176d6d31p-8, -0x1.917a6bc29b42cp-4, 0x1.e2718e0000000p-60, 0x1.0000000000000p+0},
}
