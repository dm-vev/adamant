package structure

import "fmt"

// ParseJigsawDistance decodes JigsawStructure.MaxDistance's scalar or axis-specific form.
func ParseJigsawDistance(raw any) (horizontal, vertical int32, err error) {
	const sourceYSize = 4064 // DimensionType.Y_SIZE: 12 packed Y bits, minus 32.
	if fields, ok := raw.(map[string]any); ok {
		var valid bool
		horizontal, valid = int32Value(fields["horizontal"])
		if !valid || horizontal < 1 || horizontal > 128 {
			return 0, 0, fmt.Errorf("jigsaw horizontal distance must be in [1,128]")
		}
		vertical = sourceYSize
		if value, exists := fields["vertical"]; exists {
			vertical, valid = int32Value(value)
			if !valid || vertical < 1 || vertical > sourceYSize {
				return 0, 0, fmt.Errorf("jigsaw vertical distance must be in [1,%d]", sourceYSize)
			}
		}
		return horizontal, vertical, nil
	}
	value, valid := int32Value(raw)
	if !valid || value < 1 || value > 128 {
		return 0, 0, fmt.Errorf("jigsaw distance must be in [1,128]")
	}
	return value, value, nil
}

// ParseDimensionPadding decodes the source scalar or lenient bottom/top record.
// The caller retains field presence: an explicit zero is not DimensionPadding.ZERO.
func ParseDimensionPadding(raw any) (bottom, top int32, err error) {
	if fields, ok := raw.(map[string]any); ok {
		bottom, _ = int32Value(fields["bottom"])
		top, _ = int32Value(fields["top"])
		return max(bottom, 0), max(top, 0), nil
	}
	value, valid := int32Value(raw)
	if !valid || value < 0 {
		return 0, 0, fmt.Errorf("dimension padding must be non-negative")
	}
	return value, value, nil
}
