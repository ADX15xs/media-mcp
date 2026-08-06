package supplier

import (
	"fmt"
	"sort"
	"strings"
)

// SizeTable maps aspect_ratio × resolution to the [width, height] sent to the
// backend. Providers own their own table (pixel values differ per backend);
// this type only encapsulates the lookup and validation logic, and deliberately
// holds no default policy — defaults are the caller's job.
type SizeTable map[string]map[string][2]int

// ResolveSize validates the pair and looks up the dimensions; the caller
// applies provider-specific defaults before calling.
func (t SizeTable) ResolveSize(aspectRatio, resolution string) (width, height int, err error) {
	if aspectRatio == "" || resolution == "" {
		return 0, 0, fmt.Errorf("aspect_ratio and resolution are both required")
	}
	tiers, ok := t[aspectRatio]
	if !ok {
		return 0, 0, fmt.Errorf("unsupported aspect_ratio %q (supported: %s)", aspectRatio, aspectKeys(t))
	}
	wh, ok := tiers[resolution]
	if !ok {
		return 0, 0, fmt.Errorf("unsupported resolution %q (supported: %s)", resolution, resolutionKeys(tiers))
	}
	return wh[0], wh[1], nil
}

func aspectKeys(t SizeTable) string {
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func resolutionKeys(tiers map[string][2]int) string {
	keys := make([]string, 0, len(tiers))
	for k := range tiers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
