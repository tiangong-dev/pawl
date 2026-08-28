package pawl

import "testing"

// FuzzJSONNumberAtPath exercises the untrusted JSON and path input accepted by
// the json-value builtin. A panic here would abort a quality-gate run.
func FuzzJSONNumberAtPath(f *testing.F) {
	f.Add([]byte(`{"value": 42}`), "value")
	f.Add([]byte(`{"nested":{"value": 3.14}}`), "nested.value")
	f.Add([]byte(`not json`), "value")

	f.Fuzz(func(t *testing.T, data []byte, path string) {
		_, _ = jsonNumberAtPath(data, path)
	})
}
