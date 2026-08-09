package customblock

import "testing"

func TestMaterialAmbientOcclusionEncoding(t *testing.T) {
	for _, test := range []struct {
		name     string
		material Material
		want     float32
	}{
		{"opaque default", NewMaterial("texture", OpaqueRenderMethod()), 1},
		{"alpha test default", NewMaterial("texture", AlphaTestRenderMethod()), 0},
		{"enabled", NewMaterial("texture", AlphaTestRenderMethod()).WithAmbientOcclusion(), 1},
		{"disabled", NewMaterial("texture", OpaqueRenderMethod()).WithoutAmbientOcclusion(), 0},
		{"custom", NewMaterial("texture", OpaqueRenderMethod()).WithAmbientOcclusionIntensity(4.5), 4.5},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := test.material.Encode()["ambient_occlusion"].(float32)
			if !ok {
				t.Fatalf("ambient_occlusion has type %T, want float32", test.material.Encode()["ambient_occlusion"])
			}
			if got != test.want {
				t.Errorf("ambient_occlusion = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMaterialAmbientOcclusionIntensityRange(t *testing.T) {
	material := NewMaterial("texture", OpaqueRenderMethod())
	for _, intensity := range []float32{0, 10} {
		if got := material.WithAmbientOcclusionIntensity(intensity).Encode()["ambient_occlusion"]; got != intensity {
			t.Errorf("intensity %v encoded as %v", intensity, got)
		}
	}

	for _, intensity := range []float32{-0.1, 10.1} {
		t.Run("invalid", func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("WithAmbientOcclusionIntensity(%v) did not panic", intensity)
				}
			}()
			material.WithAmbientOcclusionIntensity(intensity)
		})
	}
}
