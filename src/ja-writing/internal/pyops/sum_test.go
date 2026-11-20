package pyops

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"

	"ja-writing/internal/pyjson"
)

// sumValue は testdata/sum.json の値 1 つ (oracle/py/record_pysum.py の符号化)。
func sumValue(raw json.RawMessage) (pyjson.Value, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	switch x := v.(type) {
	case nil, bool, string:
		return x, nil
	case map[string]any:
		if s, ok := x["i"].(string); ok {
			neg := strings.HasPrefix(s, "-")
			b, ok := new(big.Int).SetString(strings.TrimPrefix(strings.TrimPrefix(s, "-"), "0x"), 16)
			if !ok {
				return nil, fmt.Errorf("整数を読めません: %s", s)
			}
			if neg {
				b.Neg(b)
			}
			return intValue(b), nil
		}
		if s, ok := x["f"].(string); ok {
			bits, err := strconv.ParseUint(s, 16, 64)
			if err != nil {
				return nil, err
			}
			return math.Float64frombits(bits), nil
		}
	}
	return nil, fmt.Errorf("知らない値: %s", raw)
}

// sumKey は結果を比べる形 (int と float を分け、float は 64 ビットそのもの。nan はどれも同じ)。
func sumKey(v pyjson.Value) string {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) {
			return "f:nan"
		}
		return fmt.Sprintf("f:%016x", math.Float64bits(x))
	case int64:
		return "i:" + big.NewInt(x).String()
	case *big.Int:
		return "i:" + x.String()
	}
	return fmt.Sprintf("?:%T", v)
}

// Sum が本家の組み込みの sum (Python 3.15.0) と同じ値・同じ型・同じ例外を返すこと。記録は oracle/py/record_pysum.py が本物の sum で作った。
func TestSumMatchesPythonRecording(t *testing.T) {
	body, err := os.ReadFile("testdata/sum.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Python string `json:"python"`
		Cases  []struct {
			Items []json.RawMessage `json:"items"`
			Sum   map[string]string `json:"sum"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(body, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Cases) < 3000 {
		t.Fatalf("記録が %d 件しかない (空振り)", len(g.Cases))
	}
	kinds := map[string]int{}
	bad := 0
	for n, c := range g.Cases {
		items := make([]pyjson.Value, len(c.Items))
		for i, raw := range c.Items {
			if items[i], err = sumValue(raw); err != nil {
				t.Fatalf("case %d: %v", n, err)
			}
		}
		var want string
		if e, ok := c.Sum["e"]; ok {
			want = "E:" + e
		} else {
			raw, _ := json.Marshal(c.Sum)
			v, err := sumValue(raw)
			if err != nil {
				t.Fatalf("case %d: %v", n, err)
			}
			want = sumKey(v)
		}
		kinds[want[:2]]++
		v, err := outcome(func() pyjson.Value { return Sum(items) })
		got := sumKey(v)
		var e *Exception
		if errors.As(err, &e) {
			got = "E:" + e.Type
		}
		if got != want {
			bad++
			if bad <= 20 {
				t.Errorf("case %d sum(%s) = %s (本家 %s)", n, formatItems(items), got, want)
			}
		}
	}
	if bad > 20 {
		t.Errorf("ほか %d 件", bad-20)
	}
	if kinds["f:"] == 0 || kinds["i:"] == 0 || kinds["E:"] == 0 {
		t.Fatalf("記録に float・int・例外の結果がそろっていない: %v", kinds)
	}
	t.Logf("%s の sum と %d 件を比べた (%v)", g.Python, len(g.Cases), kinds)
}

func formatItems(items []pyjson.Value) string {
	parts := make([]string, len(items))
	for i, v := range items {
		switch x := v.(type) {
		case float64:
			parts[i] = pyjson.FloatRepr(x)
		case string:
			parts[i] = strconv.Quote(x)
		default:
			parts[i] = fmt.Sprint(x)
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
