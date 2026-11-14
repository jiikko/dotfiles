package oracle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// DiffReportFile は diff の人が読む出力 (本家の report(d)。群 AD) の記録 (testdata/golden/ の中)。入力は本家の diff() が返す dict で、
// 実際の組の結果と、節を書き換えた dict (文面の分岐を 1 つずつ通す。diff() が作らない形の節も含む)。dict は記録の中に持つので、Go の側は組み立てずに読む。
const DiffReportFile = "diff-report.json"

// DiffReportCase は report の 1 回の呼び出し。D は report に渡した dict (JSON)、Report は本家の戻り値。
type DiffReportCase struct {
	Name   string          `json:"name"`
	D      json.RawMessage `json:"d"`
	Report string          `json:"report"`
}

// DiffReportGolden は diff-report.json の中身。
type DiffReportGolden struct {
	Header Header           `json:"header"`
	Cases  []DiffReportCase `json:"cases"`
}

// MarshalDiffReport は DiffReportGolden を決まったバイト列にする (1 行 1 case)。
func MarshalDiffReport(g DiffReportGolden) ([]byte, error) {
	return marshalHeaderCases(g.Header, g.Cases)
}

// RecordDiffReport は record_diff_report.py (本家の diff() が返す dict と、書き換えた dict) を本家の report に通して記録にする。
func (r Runner) RecordDiffReport(ctx context.Context, header Header) (DiffReportGolden, error) {
	sb, err := r.newStagedSandbox()
	if err != nil {
		return DiffReportGolden{}, err
	}
	defer sb.remove()
	in := struct {
		ScriptsDir string `json:"scripts_dir"`
	}{ScriptsDir: sb.tree.Path(ScriptsDir)}
	var out struct {
		Cases []DiffReportCase `json:"cases"`
	}
	if err := r.runDriver(ctx, sb, "record_diff_report.py", in, &out); err != nil {
		return DiffReportGolden{}, err
	}
	if len(out.Cases) == 0 {
		return DiffReportGolden{}, errors.New("diff-report の case が 0 件です")
	}
	seen := map[string]bool{}
	for _, c := range out.Cases {
		if seen[c.Name] {
			return DiffReportGolden{}, fmt.Errorf("diff-report の case の名前 %q が重なっています", c.Name)
		}
		seen[c.Name] = true
	}
	return DiffReportGolden{Header: header, Cases: out.Cases}, nil
}
