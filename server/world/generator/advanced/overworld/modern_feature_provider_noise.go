package overworld

import (
	"fmt"
	"math"
	"sync"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

// Registry holder strings name providers, not blocks. Literal states use the
// separate BlockState codec (including the strings inside weighted entries).
var javaStateProviderRegistry = sync.OnceValue(func() map[string]any {
	providers := make(map[string]any)
	for _, name := range requireJavaResourceNames("block_state_provider") {
		provider := requireJavaJSON("block_state_provider", name)
		kind, _ := provider["type"].(string)
		switch normalizeJavaResource(kind) {
		case "noise", "noise_threshold", "dual_noise":
			seed := int64(provider["seed"].(float64))
			provider["noise"] = javaStateProviderNoise(seed, provider["noise"])
			if normalizeJavaResource(kind) == "dual_noise" {
				provider["slow_noise"] = javaStateProviderNoise(seed, provider["slow_noise"])
			}
		}
		providers[name] = provider
	}
	return providers
})

func resolveJavaStateProvider(value any) any {
	name, reference := value.(string)
	if !reference {
		return value
	}
	provider, ok := javaStateProviderRegistry()[normalizeJavaResource(name)]
	if !ok {
		panic(fmt.Sprintf("overworld: unknown Java block state provider %q", name))
	}
	return provider
}

// Inline definitions share immutable noise engines too. Named providers retain
// the engine directly after registry loading, avoiding a hot-path cache lookup.
type stateProviderNoiseKey struct {
	seed                                   int64
	baseOctave, octaveCount, modifierCount int
	baseAmplitude                          float64
	normalization                          java26.NoiseNormalization
	modifiers                              [32]float64
}

var stateProviderNoises sync.Map // stateProviderNoiseKey -> func() *java26.NormalNoise

func javaStateProviderNoise(seed int64, raw any) *java26.NormalNoise {
	if noise, ok := raw.(*java26.NormalNoise); ok {
		return noise
	}
	data, ok := raw.(map[string]any)
	if !ok {
		panic("overworld: malformed state provider noise parameters")
	}
	key := stateProviderNoiseKey{seed: seed, baseAmplitude: 1, octaveCount: 1}
	key.baseOctave = featureNumber(data["base_octave"])
	if value, exists := data["base_amplitude"]; exists {
		key.baseAmplitude = value.(float64)
	}
	if value, exists := data["octave_count"]; exists {
		key.octaveCount = featureNumber(value)
	}
	switch value := data["normalize"]; value {
	case nil, true:
	case false:
		key.normalization = java26.NoiseUnnormalized
	case "legacy":
		key.normalization = java26.NoiseLegacyNormalization
	default:
		panic("overworld: invalid state provider noise normalization")
	}
	if key.baseOctave < -32 || key.baseOctave > 32 || key.octaveCount < 1 || key.octaveCount > 32 || math.IsNaN(key.baseAmplitude) || key.baseAmplitude < float64(float32(1e-5)) || key.baseAmplitude > 1e6 {
		panic("overworld: invalid state provider noise parameters")
	}
	if raw, exists := data["amplitude_modifiers"]; exists {
		values, ok := raw.([]any)
		if !ok || len(values) != 0 && len(values) != key.octaveCount {
			panic("overworld: invalid noise amplitude modifiers")
		}
		key.modifierCount = len(values)
		for i, value := range values {
			amplitude := value.(float64)
			if math.IsNaN(amplitude) || amplitude < 0 || amplitude > 1e6 {
				panic("overworld: invalid noise amplitude modifier")
			}
			key.modifiers[i] = amplitude
		}
	}
	if cached, ok := stateProviderNoises.Load(key); ok {
		return cached.(func() *java26.NormalNoise)()
	}
	create := sync.OnceValue(func() *java26.NormalNoise {
		parameters := java26.NoiseParameters{
			BaseOctave: key.baseOctave, BaseAmplitude: key.baseAmplitude, OctaveCount: key.octaveCount,
			Normalization: key.normalization, AmplitudeModifiers: key.modifiers[:key.modifierCount],
		}
		random := java26.NewLegacyRandom(key.seed)
		return java26.NewNormalNoiseParameters(&random, parameters)
	})
	cached, _ := stateProviderNoises.LoadOrStore(key, create)
	return cached.(func() *java26.NormalNoise)()
}

func stateProviderNoiseIndex(value float32, count int) int {
	placement := (float32(1) + value) / float32(2)
	placement = min(max(placement, float32(0)), float32(0.9999))
	return int(placement * float32(count))
}

func sampleJavaNoiseStateProvider(p featurePos, provider map[string]any, random *java26.Random) (javastructure.Block, bool) {
	seed := int64(provider["seed"].(float64))
	noise := javaStateProviderNoise(seed, provider["noise"])
	scale := float64(float32(provider["scale"].(float64)))
	value := noise.Sample(float64(p.x)*scale, float64(p.y)*scale, float64(p.z)*scale)
	kind := normalizeJavaResource(provider["type"].(string))
	if kind == "noise_threshold" {
		var states []any
		if value < float32(provider["threshold"].(float64)) {
			states = provider["low_states"].([]any)
		} else if random.NextFloat() < float32(provider["high_chance"].(float64)) {
			states = provider["high_states"].([]any)
		} else {
			return featureStateBlock(provider["default_state"])
		}
		return featureStateBlock(states[random.NextInt(int32(len(states)))])
	}
	states := provider["states"].([]any)
	if kind == "dual_noise" {
		slow := javaStateProviderNoise(seed, provider["slow_noise"])
		slowScale := float32(provider["slow_scale"].(float64))
		slowAt := func(p featurePos) float32 {
			// Unlike the base provider, Java multiplies these coordinates as floats.
			return slow.Sample(float64(float32(p.x)*slowScale), float64(float32(p.y)*slowScale), float64(float32(p.z)*slowScale))
		}
		low, high := int32(0), int32(0)
		switch bounds := provider["variety"].(type) {
		case float64:
			low, high = int32(bounds), int32(bounds)
		case []any:
			if len(bounds) != 2 {
				panic("overworld: malformed noise provider variety")
			}
			low, high = int32(featureNumber(bounds[0])), int32(featureNumber(bounds[1]))
		default:
			panic("overworld: malformed noise provider variety")
		}
		if low < 1 || high < low || high > 64 {
			panic("overworld: invalid noise provider variety")
		}
		fraction := min(max((float64(slowAt(p))+1)/2, float64(0)), float64(1))
		span := float64(fraction * float64(high+1-low))
		count := int(float64(low) + span)
		// The source builds a temporary list. Its samples are pure, so evaluate
		// only the selected entry without allocating or sampling unused entries.
		index := int32(stateProviderNoiseIndex(value, count))
		value = slowAt(featurePos{p.x + index*54545, p.y, p.z + index*34234})
	}
	return featureStateBlock(states[stateProviderNoiseIndex(value, len(states))])
}
