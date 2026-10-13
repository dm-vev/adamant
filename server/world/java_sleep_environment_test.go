package world

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSleepDarknessMatchesJava263Source(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Tick     int     `json:"tick"`
			Weather  string  `json:"weather"`
			SkyLight float32 `json:"skyLight"`
			Dark     bool    `json:"isDarkOutside"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/java263_sleep_environment_source.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		raining, thundering := tc.Weather != "clear", tc.Weather == "thunder"
		got := javaSleepSkyLight(tc.Tick, raining, thundering)
		if got != tc.SkyLight {
			t.Errorf("%s tick%d SKY_LIGHT_LEVEL=%v Source=%v", tc.Weather, tc.Tick, got, tc.SkyLight)
		}
		if dark := int32(float32(15)-got) >= 4; dark != tc.Dark {
			t.Errorf("%s tick%d dark=%v Source=%v", tc.Weather, tc.Tick, dark, tc.Dark)
		}
	}
}
