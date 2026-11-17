package acp

import "strings"

var canonicalEfforts = []string{"low", "medium", "high", "xhigh", "max"}

var effortDisplayLabels = map[string]string{
	"low":    "Low",
	"medium": "Medium",
	"high":   "High",
	"xhigh":  "Extra",
	"max":    "Max",
}

// StripEffortSuffix removes trailing "(low)" or "(Low thinking)" annotations from a model display label.
func StripEffortSuffix(label, effort string) string {
	display := effortDisplayLabels[effort]
	candidates := []string{effort, display}
	for _, cand := range candidates {
		if cand == "" {
			continue
		}
		suffixes := []string{
			" (" + cand + ")",
			" (" + cand + " thinking)",
		}
		for _, suffix := range suffixes {
			if len(label) > len(suffix) && strings.EqualFold(label[len(label)-len(suffix):], suffix) {
				return strings.TrimSpace(label[:len(label)-len(suffix)])
			}
		}
	}
	return label
}

// ParseLabelSlug extracts a normalized base slug and effort level from a model label or ID.
func ParseLabelSlug(label string) (base, effort string, ok bool) {
	for _, eff := range canonicalEfforts {
		stripped := StripEffortSuffix(label, eff)
		if stripped != label && stripped != "" {
			norm := strings.ToLower(strings.Join(strings.Fields(stripped), "-"))
			return norm, eff, true
		}
		suffix := "-" + eff
		if strings.HasSuffix(strings.ToLower(label), suffix) && len(label) > len(suffix) {
			return strings.ToLower(label[:len(label)-len(suffix)]), eff, true
		}
	}
	return "", "", false
}

// GroupModels groups flat model entries into ACP ConfigOption model & effort selectors.
func GroupModels(models []ModelInfo, currentModelID string) []ConfigOption {
	if len(models) == 0 {
		return nil
	}
	type family struct {
		id      string
		name    string
		efforts []string
		firstID string
	}
	var order []string
	families := make(map[string]*family)

	for _, m := range models {
		base, eff, ok := ParseLabelSlug(m.ModelID)
		if !ok {
			base, eff, ok = ParseLabelSlug(m.Name)
		}
		if !ok {
			base = m.ModelID
		}
		fam, exists := families[base]
		if !exists {
			name := StripEffortSuffix(m.Name, eff)
			if name == "" {
				name = m.ModelID
			}
			fam = &family{id: base, name: name, firstID: m.ModelID}
			families[base] = fam
			order = append(order, base)
		}
		if eff != "" {
			fam.efforts = append(fam.efforts, eff)
		}
	}

	modelValues := make([]ConfigOptionValue, 0, len(order))
	currentFam := order[0]
	currentEff := ""
	for _, key := range order {
		fam := families[key]
		modelValues = append(modelValues, ConfigOptionValue{
			Value: fam.firstID,
			Name:  fam.name,
		})
		if fam.firstID == currentModelID || key == currentModelID {
			currentFam = fam.firstID
		}
		if base, eff, ok := ParseLabelSlug(currentModelID); ok && base == key {
			currentFam = fam.firstID
			currentEff = eff
		}
	}

	opts := []ConfigOption{
		{
			ID:           "model",
			Name:         "Model",
			Category:     "model",
			Type:         "select",
			CurrentValue: currentFam,
			Options:      modelValues,
		},
	}

	// If active family exposes effort variants, emit an effort ConfigOption too.
	base, _, _ := ParseLabelSlug(currentModelID)
	if base == "" {
		base = order[0]
	}
	if fam := families[base]; fam != nil && len(fam.efforts) > 0 {
		if currentEff == "" {
			currentEff = fam.efforts[0]
		}
		var effVals []ConfigOptionValue
		for _, eff := range fam.efforts {
			label := effortDisplayLabels[eff]
			if label == "" {
				label = eff
			}
			effVals = append(effVals, ConfigOptionValue{Value: eff, Name: label})
		}
		opts = append(opts, ConfigOption{
			ID:           "thought_level",
			Name:         "Thinking Level",
			Category:     "thought_level",
			Type:         "select",
			CurrentValue: currentEff,
			Options:      effVals,
		})
	}
	return opts
}
