package ui

import (
	"encoding/json"
	"testing"
)

func FuzzParseJSONTree(f *testing.F) {
	f.Add([]byte(`{"admin":{"listen":"localhost:2019"},"apps":{"http":{"servers":{"srv0":{"listen":[":443"]}}}}}`))
	f.Add([]byte(`[1, "two", null, true, {"nested": [1,2,3]}]`))
	f.Add([]byte(`"just a string"`))
	f.Add([]byte(`42`))
	f.Add([]byte(`null`))
	f.Add([]byte(`true`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`{"deeply":{"nested":{"object":{"with":{"many":{"levels":"value"}}}}}}`))
	f.Add([]byte(`{"array_of_objects":[{"a":1},{"b":2},{"c":3}]}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var raw json.RawMessage
		if json.Unmarshal(data, &raw) != nil {
			return
		}

		root, err := parseJSONTree(raw)
		if err != nil {
			return
		}

		if root == nil {
			t.Fatal("parseJSONTree returned nil root without error")
		}

		visible := flattenVisible(root)
		for _, n := range visible {
			if n == nil {
				t.Fatal("flattenVisible returned nil node")
			}
		}

		expandAll(root)
		expanded := flattenVisible(root)
		if len(expanded) < len(visible) {
			t.Fatal("expandAll should not reduce visible nodes")
		}

		collapseAll(root)
	})
}
