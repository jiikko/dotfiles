package check

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"ja-writing/internal/pyjson"
)

// 記録 check-base.json は欄を 1 か所ずつ書き換えるので、nan が 2 つ並ぶ組を持たない。期待値は本家 (Python 3.15.0) の _record_shape の答え。
func TestRecordShapeCountsNaNNamesAsOne(t *testing.T) {
	cases := map[string][]string{
		"b01_nan_names.json":         {"checks[0].name", "checks[1].name", "checks.name", "checks.name"},
		"b02_nan_names_badmode.json": {"mode", "checks[0].name", "checks[1].name", "checks.name"},
	}
	for name, want := range cases {
		data, err := os.ReadFile(filepath.Join("testdata", "record_shape_nan", name))
		if err != nil {
			t.Fatal(err)
		}
		v, err := pyjson.LoadsBytes(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		record, ok := v.(*pyjson.Object)
		if !ok {
			t.Fatalf("%s: dict でない", name)
		}
		got, err := RecordShape(record)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: RecordShape = %q、本家は %q", name, got, want)
		}
	}
}

func TestDistinctCountMatchesPythonSet(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{"[NaN, NaN]", 1},
		{"[NaN, 1.0, 1, true]", 2},
		{"[Infinity, Infinity, NaN]", 2},
	} {
		v, err := pyjson.Loads(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if got := distinctCount(v.([]pyjson.Value)); got != c.want {
			t.Errorf("len(set(%s)) = %d、Python は %d", c.in, got, c.want)
		}
	}
}
