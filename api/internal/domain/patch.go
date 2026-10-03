package domain

import "maps"

// ValidateCustomFieldsPatch checks a custom-field patch, where a nil value
// removes its key. Every key must be valid, and the values being set must
// fit in a custom-field map on their own. Whether the merged result fits is
// a question about current state, for the caller to check.
func ValidateCustomFieldsPatch(patch map[string]any) error {
	sets := make(map[string]any, len(patch))
	for key, value := range patch {
		if err := validateCustomFieldKey(key); err != nil {
			return err
		}
		if value != nil {
			sets[key] = value
		}
	}
	return ValidateCustomFields(sets)
}

// MergePatch applies patch to current per key: a nil value removes the key,
// any other value replaces it. It returns a new map and leaves current alone.
// This is the last-write-wins rule for maps (custom fields, project metadata).
func MergePatch(current, patch map[string]any) map[string]any {
	merged := maps.Clone(current)
	if merged == nil {
		merged = make(map[string]any, len(patch))
	}
	for key, value := range patch {
		if value == nil {
			delete(merged, key)
		} else {
			merged[key] = value
		}
	}
	return merged
}
