package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// VisibilityFile は markdown_visibility の関数の記録 (testdata/golden/ の中)。
const VisibilityFile = "visibility.json"

// VisKept は結果そのものを記録に残した呼び出し (Keep)。Index は case の中の何番目の呼び出しか。
type VisKept struct {
	Index     int             `json:"i"`
	Result    json.RawMessage `json:"result,omitempty"`
	Exception *PyException    `json:"exception,omitempty"`
}

// VisCaseRecord は case 1 つの記録。Digests は呼び出しごとの結果の digest (8 字の 16 進) を呼び出しの順につないだもの。
// 例外になった呼び出しの添字は Raised。
type VisCaseRecord struct {
	Name    string    `json:"name"`
	Calls   int       `json:"calls"`
	Digests string    `json:"digests"`
	Raised  []int     `json:"raised,omitempty"`
	Kept    []VisKept `json:"kept,omitempty"`
}

// VisFunctionRecord は関数ごとの呼び出しの数と、そのうち例外になった数 (op は数えない)。
type VisFunctionRecord struct {
	Name   string `json:"name"`
	Calls  int    `json:"calls"`
	Raised int    `json:"raised"`
}

// VisibilityGolden は visibility.json の中身。
type VisibilityGolden struct {
	Header    Header              `json:"header"`
	Functions []VisFunctionRecord `json:"functions"`
	Cases     []VisCaseRecord     `json:"cases"`
}

// DigestWidth は呼び出し 1 つの digest の字数 (SHA-256 の先頭の 16 進。8 字 = 32 bit)。
// 全桁 (64 字) にしない理由: digest は記録の大半を占めるので、記録 (今 2.5 MB) が数倍に膨らみ、commit の差分を読めなくなる。
// 32 bit で足りる理由: Go の答えが誤っていても緑で通るには、その誤りが記録と同じ先頭 32 bit に当たる必要があり、誤り 1 つあたりの確率は 2^-32。
// 誤った呼び出しは 1 つの誤りが多数の呼び出しに現れるので、全部が衝突する確率はそれより小さい。食い違ったときの表示用に Keep の結果は別に持つ (一致の判定には使わない)。
// 幅を上げるなら、記録の大きさとレビューで読める差分を測り直してから。
const DigestWidth = 8

// RecordVisibility は本家の関数に cases を通して記録にする。全部の case を 1 つの Python のプロセスで順に走らせる。
func (r Runner) RecordVisibility(ctx context.Context, header Header, cases []VisCase) (VisibilityGolden, error) {
	var calls []Call
	for _, c := range cases {
		calls = append(calls, c.Calls...)
	}
	results, err := r.CallFunctions(ctx, calls)
	if err != nil {
		return VisibilityGolden{}, err
	}
	return assembleVisibility(header, cases, results)
}

// assembleVisibility は cases の呼び出しと同じ順の results を、case ごとの記録にまとめる。
func assembleVisibility(header Header, cases []VisCase, results []CallResult) (VisibilityGolden, error) {
	g := VisibilityGolden{Header: header}
	stats := map[string]*VisFunctionRecord{}
	next := 0
	for _, c := range cases {
		rec := VisCaseRecord{Name: c.Name, Calls: len(c.Calls)}
		var digests strings.Builder
		for i, call := range c.Calls {
			res := results[next]
			next++
			if len(res.Digest) != DigestWidth {
				return VisibilityGolden{}, fmt.Errorf("case %s の %d 番目の呼び出しの digest が %q です (期待は %d 字)", c.Name, i, res.Digest, DigestWidth)
			}
			if call.Op != nil && res.Raised {
				// 操作 (fixture を作る・環境を戻す) の失敗は、関数の例外と違って記録の中身ではない。Go の Fixture.Apply が同じ操作を成功させて、再生のときに初めて食い違うのを止める。
				return VisibilityGolden{}, fmt.Errorf("case %s の %d 番目の操作 %s %q が Python で失敗しました: %s", c.Name, i, call.Op.Kind, call.Op.Path, describeException(res.Exception))
			}
			digests.WriteString(res.Digest)
			if res.Raised {
				rec.Raised = append(rec.Raised, i)
			}
			if call.Keep {
				rec.Kept = append(rec.Kept, VisKept{Index: i, Result: res.Result, Exception: res.Exception})
			}
			if call.Op == nil {
				s := stats[call.Function]
				if s == nil {
					s = &VisFunctionRecord{Name: call.Function}
					stats[call.Function] = s
				}
				s.Calls++
				if res.Raised {
					s.Raised++
				}
			}
		}
		rec.Digests = digests.String()
		g.Cases = append(g.Cases, rec)
	}
	for _, s := range stats {
		g.Functions = append(g.Functions, *s)
	}
	sort.Slice(g.Functions, func(i, j int) bool { return g.Functions[i].Name < g.Functions[j].Name })
	return g, nil
}

// MarshalVisibility は VisibilityGolden を決まったバイト列にする (case ごとに 1 行)。
func MarshalVisibility(g VisibilityGolden) ([]byte, error) {
	header, err := marshalField("header", g.Header)
	if err != nil {
		return nil, err
	}
	fns, err := marshalLines(g.Functions)
	if err != nil {
		return nil, err
	}
	cases, err := marshalLines(g.Cases)
	if err != nil {
		return nil, err
	}
	return []byte("{\n" + header + ",\n\"functions\": " + fns + ",\n\"cases\": " + cases + "\n}\n"), nil
}

func describeException(e *PyException) string {
	if e == nil {
		return "(例外の中身なし)"
	}
	return e.Type + ": " + e.Message
}
