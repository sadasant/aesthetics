package guideline

import (
	"bytes"
	"embed"
	"fmt"

	"go.yaml.in/yaml/v3"
)

//go:embed presets/*.yaml
var presetFiles embed.FS

// PresetIDs lists the predefined presets in catalog order. Identifiers are
// stable; definitions may evolve without changing them.
var PresetIDs = []string{"unix", "kiss", "atomic", "readme", "diataxis", "solid"}

// Presets parses the predefined catalog.
func Presets() ([]*Set, error) {
	var out []*Set
	for _, id := range PresetIDs {
		data, err := presetFiles.ReadFile("presets/" + id + ".yaml")
		if err != nil {
			return nil, err
		}
		var s Set
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&s); err != nil {
			return nil, fmt.Errorf("preset %s: %w", id, err)
		}
		if s.ID != id {
			return nil, fmt.Errorf("preset file %s declares id %q", id, s.ID)
		}
		s.Builtin = true
		for _, spec := range s.Guidelines {
			if _, err := spec.Build(id); err != nil {
				return nil, fmt.Errorf("preset %s: %w", id, err)
			}
		}
		out = append(out, &s)
	}
	return out, nil
}
