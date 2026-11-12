package oracle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 記録のファイル名 (testdata/golden/ の中)。
const (
	PatternsFile = "patterns.json"
	SmokeFile    = "smoke.json"
)

// Header は記録を取った環境。今の環境と違うと make golden は止まる (UPDATE=1 のときだけ上書き)。
type Header struct {
	UpstreamCommit     string       `json:"upstream_commit"`
	PythonVersion      string       `json:"python_version"`
	UnicodedataVersion string       `json:"unicodedata_version"`
	Corpus             []CorpusFile `json:"corpus,omitempty"`
}

// NewHeader は今の環境と入力の集まりから Header を作る。
func NewHeader(info PyInfo, c Corpus) Header {
	return Header{UpstreamCommit: Commit, PythonVersion: info.PythonVersion, UnicodedataVersion: info.UnicodedataVersion, Corpus: c.Files}
}

// WithoutCorpus は入力の集まりの一覧を外した Header (入力に依らない記録 = smoke に使う)。
func (h Header) WithoutCorpus() Header {
	h.Corpus = nil
	return h
}

// EnvMismatch は記録の環境 (本家の commit・Python・unicodedata のバージョン) と今の環境の違いを返す。同じなら空。
// 入力の集まりの違いは含めない (入力を足したときの作り直しは UPDATE を要らなくする)。
func (h Header) EnvMismatch(now Header) string {
	var diffs []string
	for _, d := range [][3]string{
		{"本家の commit", h.UpstreamCommit, now.UpstreamCommit},
		{"Python", h.PythonVersion, now.PythonVersion},
		{"unicodedata", h.UnicodedataVersion, now.UnicodedataVersion},
	} {
		if d[1] != d[2] {
			diffs = append(diffs, fmt.Sprintf("%s: 記録 %s / 今 %s", d[0], d[1], d[2]))
		}
	}
	return strings.Join(diffs, "、")
}

// PatternsGolden は patterns.json の中身。
type PatternsGolden struct {
	Header   Header          `json:"header"`
	Runs     []ToolRun       `json:"runs"`
	Patterns []PatternRecord `json:"patterns"`
}

// SmokeGolden は smoke.json の中身。関数と CLI の driver が動くことの確認と、1074 以降が記録を足すときの見本を兼ねる。
type SmokeGolden struct {
	Header    Header      `json:"header"`
	Functions []SmokeCall `json:"functions"`
	CLIs      []SmokeCLI  `json:"clis"`
}

// SmokeCall は関数の呼び出しとその結果。
type SmokeCall struct {
	Call   Call       `json:"call"`
	Result CallResult `json:"result"`
}

// SmokeCLI は CLI の実行とその結果。
type SmokeCLI struct {
	Spec   CLISpec   `json:"spec"`
	Result CLIResult `json:"result"`
}

// marshalLines は配列の要素を 1 行に 1 つずつ書く (記録の差分を行単位で読めるようにする)。
func marshalLines[T any](items []T) (string, error) {
	if len(items) == 0 {
		return "[]", nil
	}
	var b strings.Builder
	b.WriteString("[\n")
	for i, it := range items {
		j, err := json.Marshal(it)
		if err != nil {
			return "", fmt.Errorf("記録を JSON にできません: %w", err)
		}
		b.WriteString("  " + string(j))
		if i < len(items)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("]")
	return b.String(), nil
}

func marshalField(name string, v any) (string, error) {
	j, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("%s を JSON にできません: %w", name, err)
	}
	return fmt.Sprintf("%q: %s", name, j), nil
}

// MarshalPatterns は PatternsGolden を決まったバイト列にする (キーの順・1 行 1 要素。同じ記録は同じバイト列になる)。
func MarshalPatterns(g PatternsGolden) ([]byte, error) {
	header, err := marshalField("header", g.Header)
	if err != nil {
		return nil, err
	}
	runs, err := marshalLines(g.Runs)
	if err != nil {
		return nil, err
	}
	patterns, err := marshalLines(g.Patterns)
	if err != nil {
		return nil, err
	}
	return []byte("{\n" + header + ",\n\"runs\": " + runs + ",\n\"patterns\": " + patterns + "\n}\n"), nil
}

// MarshalSmoke は SmokeGolden を決まったバイト列にする。
func MarshalSmoke(g SmokeGolden) ([]byte, error) {
	header, err := marshalField("header", g.Header)
	if err != nil {
		return nil, err
	}
	fns, err := marshalLines(g.Functions)
	if err != nil {
		return nil, err
	}
	clis, err := marshalLines(g.CLIs)
	if err != nil {
		return nil, err
	}
	return []byte("{\n" + header + ",\n\"functions\": " + fns + ",\n\"clis\": " + clis + "\n}\n"), nil
}

// ReadGolden は dir/name の JSON を v に読む。未知のフィールドがあればエラー (形が変わったのに読み側が追従していない)。
// ファイルが無いときは errors.Is(err, os.ErrNotExist) で見分けられる。
func ReadGolden(dir, name string, v any) error {
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("記録 %s を読めません: %w", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("記録 %s を読めません: %w", name, err)
	}
	return nil
}

// ReadGoldenHeader は記録の header だけを読む (header 以外のフィールドは読まない)。
func ReadGoldenHeader(dir, name string, v any) error {
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("記録 %s を読めません: %w", name, err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("記録 %s を読めません: %w", name, err)
	}
	return nil
}
