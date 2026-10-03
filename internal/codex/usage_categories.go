package codex

import "sort"

type UsageCategory struct {
	Name  string
	Value float64
}

// UsageCategories returns a deterministic ordering for a reported dimension.
func UsageCategories(groups map[string]map[string]float64, dimension string) []UsageCategory {
	result := []UsageCategory{}
	for name, value := range groups[dimension] {
		result = append(result, UsageCategory{name, value})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Value == result[j].Value {
			return result[i].Name < result[j].Name
		}
		return result[i].Value > result[j].Value
	})
	return result
}
