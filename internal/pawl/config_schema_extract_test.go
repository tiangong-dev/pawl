package pawl

import (
	"reflect"
	"sort"
	"testing"
)

func TestSchemaExtractScalarFormsMatchParser(t *testing.T) {
	extract := loadConfigSchema(t).property(t, "dimensions").Items.property(t, "extract")
	if len(extract.OneOf) != 2 {
		t.Fatalf("extract schema has %d branches, want scalar and object", len(extract.OneOf))
	}
	got := append([]string(nil), extract.OneOf[0].Enum...)
	want := []string{extractNumber, extractLines, extractExitCode}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema scalar forms = %v, want %v", got, want)
	}
	for _, form := range got {
		if _, err := parseExtract(form); err != nil {
			t.Errorf("schema accepts %q but parser rejects it: %v", form, err)
		}
	}
}
