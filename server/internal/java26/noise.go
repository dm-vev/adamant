package java26

import (
	"math"
	"strconv"
)

var gradients = [16][3]float32{
	{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0},
	{1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1},
	{1, 1, 0}, {0, -1, 1}, {-1, 1, 0}, {0, -1, -1},
}

// Perlin is Java 26.3's gradient noise: double coordinates, float interpolation.
type Perlin struct {
	perm       [256]byte
	ox, oy, oz float64
}

func newPerlin(random *Random) Perlin {
	p := Perlin{ox: random.NextDouble() * 256, oy: random.NextDouble() * 256, oz: random.NextDouble() * 256}
	for i := range p.perm {
		p.perm[i] = byte(i)
	}
	for i := range p.perm {
		j := i + int(random.NextInt(int32(256-i)))
		p.perm[i], p.perm[j] = p.perm[j], p.perm[i]
	}
	return p
}

func (p *Perlin) permute(x int) int { return int(p.perm[uint8(x)]) }

func wrap(x float64) float64 {
	const halfRoundOff = 16777215.999999998
	if x >= -halfRoundOff && x < halfRoundOff {
		return x
	}
	return x - math.Floor(x/33554432+0.5)*33554432
}

func (p *Perlin) Sample(x, y, z float64) float64 { return float64(p.sample(x, y, z)) }

func (p *Perlin) sample(x, y, z float64) float32 {
	x, y, z = wrap(x)+p.ox, wrap(y)+p.oy, wrap(z)+p.oz
	fx, fy, fz := math.Floor(x), math.Floor(y), math.Floor(z)
	rx, ry, rz := float32(x-fx), float32(y-fy), float32(z-fz)
	return p.sampleAndLerp(int(fx), int(fy), int(fz), rx, ry, rz, ry)
}

func (p *Perlin) sampleSmeared(x, y, z, smear float64) float32 {
	originalY := y
	x, y, z = wrap(x)+p.ox, wrap(y)+p.oy, wrap(z)+p.oz
	fx, fy, fz := math.Floor(x), math.Floor(y), math.Floor(z)
	relativeY := y - fy
	limit := relativeY
	if originalY >= 0 && originalY < relativeY {
		limit = originalY
	}
	fudge := math.Floor(limit/smear+float64(float32(1.0e-7))) * smear
	return p.sampleAndLerp(int(fx), int(fy), int(fz), float32(x-fx), float32(relativeY-fudge), float32(z-fz), float32(relativeY))
}

func (p *Perlin) sampleAndLerp(x, y, z int, rx, ry, rz, originalY float32) float32 {
	x0, x1 := p.permute(x), p.permute(x+1)
	xy00, xy01 := p.permute(x0+y), p.permute(x0+y+1)
	xy10, xy11 := p.permute(x1+y), p.permute(x1+y+1)
	d000 := gradientDot(p.permute(xy00+z), rx, ry, rz)
	d100 := gradientDot(p.permute(xy10+z), rx-1, ry, rz)
	d010 := gradientDot(p.permute(xy01+z), rx, ry-1, rz)
	d110 := gradientDot(p.permute(xy11+z), rx-1, ry-1, rz)
	d001 := gradientDot(p.permute(xy00+z+1), rx, ry, rz-1)
	d101 := gradientDot(p.permute(xy10+z+1), rx-1, ry, rz-1)
	d011 := gradientDot(p.permute(xy01+z+1), rx, ry-1, rz-1)
	d111 := gradientDot(p.permute(xy11+z+1), rx-1, ry-1, rz-1)
	ax, ay, az := smoothstep(rx), smoothstep(originalY), smoothstep(rz)
	low := noiseLerp(ay, noiseLerp(ax, d000, d100), noiseLerp(ax, d010, d110))
	high := noiseLerp(ay, noiseLerp(ax, d001, d101), noiseLerp(ax, d011, d111))
	return noiseLerp(az, low, high)
}

func gradientDot(hash int, x, y, z float32) float32 {
	g := gradients[hash&15]
	return g[0]*x + g[1]*y + g[2]*z
}

func smoothstep(t float32) float32      { return t * t * t * (t*(t*6-15) + 10) }
func noiseLerp(t, a, b float32) float32 { return a + t*(b-a) }

// NoiseNormalization selects the modes exposed by NormalNoise.Parameters.
type NoiseNormalization uint8

const (
	NoiseNormalized NoiseNormalization = iota
	NoiseUnnormalized
	NoiseLegacyNormalization
)

// NoiseParameters is the current Java NormalNoise builder representation.
// An empty AmplitudeModifiers means one for every octave.
type NoiseParameters struct {
	BaseOctave         int
	BaseAmplitude      float64
	OctaveCount        int
	Normalization      NoiseNormalization
	AmplitudeModifiers []float64
}

type noiseLayer struct {
	perlin    Perlin
	frequency float64
	amplitude float32
}

// NormalNoise is a flattened immutable Java NoiseStack. Interleaved octaves and
// per-layer float gains retain the current runtime's accumulation semantics.
type NormalNoise struct{ layers []noiseLayer }

func NewNormalNoise(random *Random, firstOctave int, amplitudes ...float64) *NormalNoise {
	return NewNormalNoiseParameters(random, parityNoiseParameters(firstOctave, amplitudes))
}

func NewNormalNoiseParameters(random *Random, parameters NoiseParameters) *NormalNoise {
	validateNoiseParameters(parameters)
	normalization := noiseNormalizationFactor(parameters)
	first, second := random.ForkPositional(), random.ForkPositional()
	layers := make([]noiseLayer, 0, parameters.OctaveCount*2)
	frequency, amplitude := math.Ldexp(1, parameters.BaseOctave), noiseBaseAmplitude(parameters)
	for octave := range parameters.OctaveCount {
		modifier := noiseAmplitudeModifier(parameters, octave)
		if modifier != 0 {
			name := "octave_" + strconv.Itoa(parameters.BaseOctave+octave)
			firstRandom, secondRandom := first.FromHashOf(name), second.FromHashOf(name)
			gain := float32(normalization * (amplitude * modifier))
			layers = append(layers, noiseLayer{newPerlin(&firstRandom), frequency, gain}, noiseLayer{newPerlin(&secondRandom), frequency * 1.0181268882175227, gain})
		}
		frequency *= 2
		amplitude *= 0.5
	}
	return &NormalNoise{layers: layers}
}

func parityNoiseParameters(firstOctave int, amplitudes []float64) NoiseParameters {
	if len(amplitudes) == 0 {
		panic("java26: normal noise needs at least one amplitude")
	}
	parameters := NoiseParameters{BaseOctave: firstOctave, BaseAmplitude: 1, OctaveCount: len(amplitudes), AmplitudeModifiers: amplitudes}
	normalization := noiseNormalizationFactor(parameters)
	if normalization != 0 {
		parameters.BaseAmplitude = parityNormalizationFactor(parameters) / normalization
	}
	return parameters
}

func validateNoiseParameters(parameters NoiseParameters) {
	if parameters.OctaveCount < 1 || parameters.OctaveCount > 32 || len(parameters.AmplitudeModifiers) != 0 && len(parameters.AmplitudeModifiers) != parameters.OctaveCount {
		panic("java26: invalid normal noise octave configuration")
	}
}

func noiseAmplitudeModifier(parameters NoiseParameters, index int) float64 {
	if len(parameters.AmplitudeModifiers) == 0 {
		return 1
	}
	return parameters.AmplitudeModifiers[index]
}

func noiseBaseAmplitude(parameters NoiseParameters) float64 {
	amplitude := parameters.BaseAmplitude
	if parameters.Normalization != NoiseUnnormalized {
		amplitude *= math.Ldexp(1, parameters.OctaveCount-1) / (math.Ldexp(1, parameters.OctaveCount) - 1)
	}
	return amplitude
}

func noiseNormalizationFactor(parameters NoiseParameters) float64 {
	amplitude := noiseBaseAmplitude(parameters)
	target, variance := 0.0, 0.0
	for octave := range parameters.OctaveCount {
		weight := math.Abs(amplitude * noiseAmplitudeModifier(parameters, octave))
		target += weight
		deviation := 0.2702247831245211 * weight
		variance += deviation * deviation
		amplitude *= 0.5
	}
	if variance == 0 {
		return 0
	}
	if parameters.Normalization == NoiseLegacyNormalization {
		return parityNormalizationFactor(parameters)
	}
	return target * 0.3333333333333333 / (math.Sqrt(variance) * math.Sqrt2)
}

func parityNormalizationFactor(parameters NoiseParameters) float64 {
	minimum, maximum := math.MaxInt32, math.MinInt32
	for octave := range parameters.OctaveCount {
		if noiseAmplitudeModifier(parameters, octave) != 0 {
			minimum, maximum = min(minimum, octave), max(maximum, octave)
		}
	}
	if minimum == math.MaxInt32 {
		return 0
	}
	deviation := 0.1 * (1 + 1/float64(maximum-minimum+1))
	return parameters.BaseAmplitude * 0.5 * 0.3333333333333333 / deviation
}

func NewLegacyNetherNoise(random *Random, firstOctave int, amplitudes ...float64) *NormalNoise {
	parameters := parityNoiseParameters(firstOctave, amplitudes)
	gain := float32(noiseNormalizationFactor(parameters) * parameters.BaseAmplitude)
	layers := make([]noiseLayer, 0, len(amplitudes)*2)
	layers = appendLegacyNoiseLayers(layers, random, firstOctave, amplitudes, 1, gain)
	layers = appendLegacyNoiseLayers(layers, random, firstOctave, amplitudes, 1.0181268882175227, gain)
	return &NormalNoise{layers: layers}
}

func appendLegacyNoiseLayers(layers []noiseLayer, random *Random, firstOctave int, amplitudes []float64, scale float64, gain float32) []noiseLayer {
	count, zero := len(amplitudes), -firstOctave
	if count > 32 || zero < count-1 {
		panic("java26: unsupported positive legacy noise octave")
	}
	var levels [32]Perlin
	var present [32]bool
	zeroNoise := newPerlin(random)
	if zero >= 0 && zero < count && amplitudes[zero] != 0 {
		levels[zero], present[zero] = zeroNoise, true
	}
	for octave := zero - 1; octave >= 0; octave-- {
		if octave < count && amplitudes[octave] != 0 {
			levels[octave], present[octave] = newPerlin(random), true
		} else {
			random.Consume(262)
		}
	}
	frequency := math.Ldexp(1, firstOctave)
	amplitude := math.Ldexp(1, count-1) / (math.Ldexp(1, count) - 1)
	for octave := range count {
		if present[octave] {
			layers = append(layers, noiseLayer{levels[octave], frequency * scale, float32(amplitude*amplitudes[octave]) * gain})
		}
		frequency *= 2
		amplitude *= 0.5
	}
	return layers
}

func (n *NormalNoise) Sample(x, y, z float64) float32 {
	var value float32
	for i := range n.layers {
		layer := &n.layers[i]
		value += float32(layer.amplitude * layer.perlin.sample(x*layer.frequency, y*layer.frequency, z*layer.frequency))
	}
	return value
}

func (n *NormalNoise) Sample64(x, y, z float64) float64 { return float64(n.Sample(x, y, z)) }

type smearedNoiseLayer struct {
	perlin           Perlin
	frequency, smear float64
	amplitude        float32
}

type BlendedNoise struct {
	low, high                                [16]smearedNoiseLayer
	main                                     [8]smearedNoiseLayer
	xzScale, yScale, mainXZScale, mainYScale float64
}

func NewBlendedNoise(random *Random, xzScale, yScale, xzFactor, yFactor, smearScale float64) *BlendedNoise {
	n := &BlendedNoise{xzScale: 684.412 * xzScale, yScale: 684.412 * yScale}
	n.mainXZScale, n.mainYScale = n.xzScale/xzFactor, n.yScale/yFactor
	limitSmear := n.yScale * smearScale
	fillSmearedLayers(n.low[:], random, limitSmear, float64(float32(0.99998474)))
	fillSmearedLayers(n.high[:], random, limitSmear, float64(float32(0.99998474)))
	fillSmearedLayers(n.main[:], random, limitSmear/yFactor, 12.75)
	return n
}

func fillSmearedLayers(layers []smearedNoiseLayer, random *Random, smear, valueFactor float64) {
	frequency := 1.0
	valueFactor /= math.Ldexp(1, len(layers)) - 1
	for i := range layers {
		layers[i] = smearedNoiseLayer{perlin: newPerlin(random), frequency: frequency, smear: smear * frequency, amplitude: float32(valueFactor)}
		frequency *= 0.5
		valueFactor *= 2
	}
}

func sampleSmearedLayers(layers []smearedNoiseLayer, x, y, z float64) float32 {
	var value float32
	for i := range layers {
		layer := &layers[i]
		value += float32(layer.amplitude * layer.perlin.sampleSmeared(x*layer.frequency, y*layer.frequency, z*layer.frequency, layer.smear))
	}
	return value
}

func (n *BlendedNoise) Sample(x, y, z int32) float32 {
	choice := sampleSmearedLayers(n.main[:], float64(x)*n.mainXZScale, float64(y)*n.mainYScale, float64(z)*n.mainXZScale) + 0.5
	nx, ny, nz := float64(x)*n.xzScale, float64(y)*n.yScale, float64(z)*n.xzScale
	if choice <= 0 {
		return sampleSmearedLayers(n.low[:], nx, ny, nz)
	}
	if choice >= 1 {
		return sampleSmearedLayers(n.high[:], nx, ny, nz)
	}
	return noiseLerp(choice, sampleSmearedLayers(n.low[:], nx, ny, nz), sampleSmearedLayers(n.high[:], nx, ny, nz))
}
