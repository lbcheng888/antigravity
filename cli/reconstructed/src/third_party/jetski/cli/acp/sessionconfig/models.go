// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x102c4e560 | Size: 16 funcs (first func stripEffortSuffix, 448 B) | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L46
// Package sessionconfig implements ACP session configuration options and model/thinking-level resolution.
// Reconstructed from agy v1.5.10 binary (third_party/jetski/cli/acp/sessionconfig/models.go).
package sessionconfig

import (
	"fmt"
	"strings"

	"google3/third_party/jetski/cli/acp/schema/schema"
	"google3/third_party/jetski/cli/backend/backend"
)

// effortDisplayLabels maps canonical effort slugs to display labels.
// Binary VA: 0x102c4e3a0 (init) | Size: 448 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L31
var effortDisplayLabels /* inferred */ = map[string]string{
	"low":    "Low",
	"medium": "Medium",
	"high":   "High",
	"xhigh":  "Extra",
	"max":    "Max",
}

// stripEffortSuffix removes a trailing effort suffix such as " (low)" or " (Low thinking)" from label.
// Binary VA: 0x102c4e560 | Size: 448 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L46
func stripEffortSuffix(label, effort string) string {
	displayLabel := effortDisplayLabels[effort]
	candidates /* inferred */ := []string{effort, displayLabel}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		suffixes /* inferred */ := []string{
			" (" + candidate + ")",
			" (" + candidate + " thinking)",
		}
		for _, suffix := range suffixes {
			if len(label) > len(suffix) && strings.EqualFold(label[len(label)-len(suffix):], suffix) {
				return strings.TrimSpace(label[:len(label)-len(suffix)])
			}
		}
	}
	return label
}

// parseLabelSlug extracts a normalized base slug and effort from a model label.
// Binary VA: 0x102c4e720 | Size: 384 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L62
func parseLabelSlug(label string) (string, string, bool) {
	// effort.go:48 inlined: iterates over canonical effort list (len=5)
	for _, effort := range backend.ValidEfforts() /* inferred */ {
		stripped := stripEffortSuffix(label, effort)
		if stripped == label || stripped == "" {
			continue
		}
		base := strings.ToLower(strings.Join(strings.Fields(stripped), "-"))
		return base, effort, true
	}
	return "", "", false
}

// modelSlugParts returns the base slug and effort for m, falling back to label parsing.
// Binary VA: 0x102c4e8a0 | Size: 96 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L75
func modelSlugParts(m backend.AgentModel) (string, string, bool) {
	if base, effort, ok := backend.slugFor(m.ModelID); ok {
		return base, effort, true
	}
	return parseLabelSlug(m.Label)
}

type modelFamily struct {
	base     string
	name     string
	variants []modelVariant
}

type modelVariant struct {
	effort string
	model  backend.AgentModel
}

type groupedModelEntry struct {
	family *modelFamily
	model  backend.AgentModel
}

// optionID returns the ACP config option value ID for the model family (inlined in callers).
// Source: third_party/jetski/cli/acp/sessionconfig/models.go:L104
func (f *modelFamily) optionID /* inferred */ () string {
	if f.base == "" || f.base == f.name {
		return "model:" + f.name
	}
	return f.base
}

// variantForEffort finds the variant matching effort (case-insensitive, trimmed).
// Binary VA: 0x102c4e900 | Size: 400 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L111
func (f *modelFamily) variantForEffort(effort string) (modelVariant, bool) {
	normalized := strings.ToLower(strings.TrimSpace(effort))
	for _, v := range f.variants {
		if v.effort == normalized {
			return v, true
		}
	}
	return modelVariant{}, false
}

// effortForModel returns the effort slug corresponding to m within the family.
// Binary VA: 0x102c4ea90 | Size: 352 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L121
func (f *modelFamily) effortForModel(m backend.AgentModel) (string, bool) {
	targetID := ModelOptionID(m)
	for _, v := range f.variants {
		if ModelOptionID(v.model) == targetID {
			return v.effort, true
		}
	}
	return "", false
}

// efforts returns the canonical list of efforts offered by the family.
// Binary VA: 0x102c4ebf0 | Size: 576 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L132
func (f *modelFamily) efforts() []string {
	var out []string
	// effort.go:48 inlined: canonical effort order
	for _, effort := range backend.ValidEfforts() /* inferred */ {
		if _, ok := f.variantForEffort(effort); ok {
			out = append(out, effort)
		}
	}
	return out
}

// defaultModel selects the default variant model for the family.
// Binary VA: 0x102c4ee30 | Size: 544 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L146
func (f *modelFamily) defaultModel(cfg *backend.ModelConfig) backend.AgentModel {
	effort := backend.DefaultEffortForBase(cfg, f.base)
	if _, ok := f.variantForEffort(effort); !ok {
		efforts := f.efforts()
		// effort.go:347-355 inlined: prefer "medium", else first offered effort
		effort = ""
		for _, e := range efforts {
			if e == "medium" {
				effort = e
				break
			}
		}
		if effort == "" && len(efforts) > 0 {
			effort = efforts[0]
		}
	}
	v, _ := f.variantForEffort(effort)
	return v.model
}

// nearestEffortModel finds the variant in f closest to targetEffort in canonical effort order.
// Binary VA: 0x102c4f050 | Size: 1248 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L157
func (f *modelFamily) nearestEffortModel(targetEffort string) (backend.AgentModel, bool) {
	if v, ok := f.variantForEffort(targetEffort); ok {
		return v.model, true
	}
	normalized := strings.ToLower(strings.TrimSpace(targetEffort))
	effortOrder := backend.ValidEfforts() /* inferred */
	targetIdx := -1
	for i, e := range effortOrder {
		if e == normalized {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 || len(f.variants) == 0 {
		return backend.AgentModel{}, false
	}

	bestIdx := -1
	bestOrder := 0
	bestDist := 0
	for i, v := range f.variants {
		idx := -1
		for j, e := range effortOrder {
			if e == v.effort {
				idx = j
				break
			}
		}
		if idx < 0 {
			continue
		}
		dist := idx - targetIdx
		if dist < 0 {
			dist = -dist
		}
		if bestIdx == -1 || dist < bestDist || (dist == bestDist && idx > bestOrder) {
			bestIdx = i
			bestOrder = idx
			bestDist = dist
		}
	}
	if bestIdx == -1 {
		return backend.AgentModel{}, false
	}
	return f.variants[bestIdx].model, true
}

// groupModelEntries groups models with multiple effort variants into modelFamily entries.
// Binary VA: 0x102c4f530 | Size: 2352 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L198
func groupModelEntries(cfg *backend.ModelConfig) ([]groupedModelEntry, map[string]*modelFamily) {
	if cfg == nil || len(cfg.Models) == 0 {
		return nil, nil
	}
	variantsByBase := make(map[string][]modelVariant)
	grouped := make([]bool, len(cfg.Models))
	knownIDs := make(map[string]bool)

	for i, m := range cfg.Models {
		knownIDs[ModelOptionID(m)] = true
		if m.ModelID != "" {
			knownIDs[m.ModelID] = true
		}
		if m.UserFacingSlug != "" {
			knownIDs[m.UserFacingSlug] = true
		}
		base, effort, ok := modelSlugParts(m)
		if !ok {
			continue
		}
		duplicate := false
		for _, v := range variantsByBase[base] {
			if v.effort == effort {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		variantsByBase[base] = append(variantsByBase[base], modelVariant{
			effort: effort,
			model:  m,
		})
		grouped[i] = true
	}

	entries := make([]groupedModelEntry, 0, len(cfg.Models))
	families := make(map[string]*modelFamily)
	for i, m := range cfg.Models {
		base, effort, ok := modelSlugParts(m)
		if !ok || !grouped[i] || len(variantsByBase[base]) < 2 || knownIDs[base] {
			entries = append(entries, groupedModelEntry{model: m})
			continue
		}
		if _, exists := families[base]; exists {
			continue
		}
		name := stripEffortSuffix(m.Label, effort)
		if name == "" {
			name = base
		}
		fam := &modelFamily{
			base:     base,
			name:     name,
			variants: variantsByBase[base],
		}
		families[base] = fam
		entries = append(entries, groupedModelEntry{family: fam})
	}
	return entries, families
}

type unknownModelError struct {
	ModelID string
}

// Error implements error for unknownModelError.
// Binary VA: 0x102c4fe60 | Size: 112 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L266
func (e *unknownModelError) Error() string {
	return fmt.Sprintf("Unknown model ID: %s", e.ModelID)
}

type thoughtLevelNotOfferedError struct {
	Effort    string
	ModelName string
}

// Error implements error for thoughtLevelNotOfferedError.
// Binary VA: 0x102c4fed0 | Size: 144 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L280
func (e *thoughtLevelNotOfferedError) Error() string {
	return fmt.Sprintf("Thinking level %q is not offered for model %q", e.Effort, e.ModelName)
}

type noActiveModelError struct {
	Effort string
}

// Error implements error for noActiveModelError.
// Binary VA: 0x102c4ff60 | Size: 112 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L292
func (e *noActiveModelError) Error() string {
	return fmt.Sprintf("Thinking level %q is not available without an active model", e.Effort)
}

// ResolveModelSelection resolves a requested modelID against grouped model families and individual models.
// Binary VA: 0x102c4ffd0 | Size: 1328 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L299
func ResolveModelSelection(cfg *backend.ModelConfig, current backend.AgentModel, hasCurrent bool, modelID string) (backend.AgentModel, error) {
	if cfg == nil {
		return backend.AgentModel{}, &unknownModelError{ModelID: modelID}
	}
	_, families := groupModelEntries(cfg)
	for _, fam := range families {
		if fam.optionID() == modelID {
			if hasCurrent {
				if _, effort, ok := modelSlugParts(current); ok {
					if m, ok := fam.nearestEffortModel(effort); ok {
						return m, nil
					}
				}
			}
			return fam.defaultModel(cfg), nil
		}
	}
	for _, m := range cfg.Models {
		if ModelOptionID(m) == modelID || m.PreferredID() == modelID || m.ModelID == modelID || m.UserFacingSlug == modelID {
			return m, nil
		}
	}
	return backend.AgentModel{}, &unknownModelError{ModelID: modelID}
}

// ResolveThoughtLevelSelection resolves a requested thinking effort for the active model.
// Binary VA: 0x102c50500 | Size: 1040 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L332
func ResolveThoughtLevelSelection(cfg *backend.ModelConfig, current backend.AgentModel, hasCurrent bool, effort string) (backend.AgentModel, error) {
	if !hasCurrent {
		return backend.AgentModel{}, &noActiveModelError{Effort: effort}
	}
	base, _, ok := modelSlugParts(current)
	if !ok {
		return backend.AgentModel{}, &thoughtLevelNotOfferedError{
			Effort:    effort,
			ModelName: current.Label,
		}
	}
	_, families := groupModelEntries(cfg)
	fam := families[base]
	if fam == nil {
		return backend.AgentModel{}, &thoughtLevelNotOfferedError{
			Effort:    effort,
			ModelName: current.Label,
		}
	}
	v, ok := fam.variantForEffort(effort)
	if !ok {
		return backend.AgentModel{}, &thoughtLevelNotOfferedError{
			Effort:    effort,
			ModelName: fam.name,
		}
	}
	return v.model, nil
}

// ModelOptions builds the ACP "model" and (if applicable) "thought_level" config options.
// Binary VA: 0x102c50910 | Size: 2160 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L354
func ModelOptions(cfg *backend.ModelConfig, current backend.AgentModel) []schema.ConfigOption {
	if cfg == nil || len(cfg.Models) == 0 {
		return nil
	}
	entries, families := groupModelEntries(cfg)
	var currentFamily *modelFamily
	var currentEffort string
	if base, _, ok := modelSlugParts(current); ok {
		if fam := families[base]; fam != nil {
			if effort, ok := fam.effortForModel(current); ok {
				currentFamily = fam
				currentEffort = effort
			}
		}
	}

	modelValues := make([]schema.ConfigOptionValue, 0, len(entries))
	for _, entry := range entries {
		if entry.family != nil {
			modelValues = append(modelValues, schema.ConfigOptionValue{
				Value: entry.family.optionID(),
				Name:  entry.family.name,
			})
		} else {
			modelValues = append(modelValues, schema.ConfigOptionValue{
				Value:       ModelOptionID(entry.model),
				Name:        entry.model.Label,
				Description: entry.model.Description,
			})
		}
	}

	currentModelValue := ModelOptionID(current)
	if currentFamily != nil {
		currentModelValue = currentFamily.optionID()
	}

	options := []schema.ConfigOption{
		{
			ID:           "model",
			Name:         "Model",
			Category:     "model",
			Type:         "select",
			CurrentValue: currentModelValue,
			Options:      modelValues,
		},
	}

	if currentFamily != nil {
		var effortValues []schema.ConfigOptionValue
		for _, effort := range currentFamily.efforts() {
			v, _ := currentFamily.variantForEffort(effort)
			effortValues = append(effortValues, schema.ConfigOptionValue{
				Value:       effort,
				Name:        effortDisplayLabels[effort],
				Description: v.model.Description,
			})
		}
		options = append(options, schema.ConfigOption{
			ID:           "thought_level",
			Name:         "Thinking Level",
			Category:     "thought_level",
			Type:         "select",
			CurrentValue: currentEffort,
			Options:      effortValues,
		})
	}
	return options
}

// ModelOptionID returns the ACP option value identifier for m.
// Binary VA: 0x102c51180 | Size: 272 B | Source: third_party/jetski/cli/acp/sessionconfig/models.go:L429
func ModelOptionID(m backend.AgentModel) string {
	if id := m.PreferredID(); id != "" && id != m.ModelID {
		return id
	}
	if m.ModelOrAlias != nil {
		if name := m.ModelOrAlias.GetModel().String(); name != "" && name != m.ModelID {
			return name
		}
	}
	return "model:" + m.ModelID
}
