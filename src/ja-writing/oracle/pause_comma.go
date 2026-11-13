package oracle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// PauseCommaFile は lint の群 G13 (pause_comma) の正解の記録 (testdata/golden/ の中)。本家の _pause_comma_hits・_pause_comma_object_hits・
// _pause_comma_line_hits の結果。
const PauseCommaFile = "pause_comma.json"

// PauseHit は (位置, かたまり, 句) ([pos, chunk, phrase])。
type PauseHit struct {
	Pos    int
	Chunk  WStr
	Phrase WStr
}

// MarshalJSON は [pos, chunk, phrase] の形にする。
func (h PauseHit) MarshalJSON() ([]byte, error) { return marshalTuple(h.Pos, h.Chunk, h.Phrase) }

// UnmarshalJSON は [pos, chunk, phrase] を読む。
func (h *PauseHit) UnmarshalJSON(b []byte) error {
	return unmarshalTuple(b, &h.Pos, &h.Chunk, &h.Phrase)
}

// PauseStrCase は 1 つの文字列と、本家の str の 2 関数の結果。Line は入力の集まりのファイルの "\n" で割った行の添字 (Text が空のとき)。
type PauseStrCase struct {
	Line    int        `json:"line,omitempty"`
	Text    WStr       `json:"text,omitempty"`
	Hits    []PauseHit `json:"hits"`
	Objects []PauseHit `json:"objects"`
}

// PauseLineTable は _pause_comma_line_hits の表。Keys は表にある行番号の全部 (当たりが無くても表にある行は、lint_text が行ごとに当て直さない)、
// Hits は当たりのある行の [行番号, [当たり...]]。
type PauseLineTable struct {
	Keys []int            `json:"keys"`
	Hits []PauseLineEntry `json:"hits"`
}

// PauseLineEntry は [行番号, [当たり...]]。
type PauseLineEntry struct {
	Line int
	Hits []PauseHit
}

// MarshalJSON は [line, [hits...]] の形にする。
func (e PauseLineEntry) MarshalJSON() ([]byte, error) { return marshalTuple(e.Line, e.Hits) }

// UnmarshalJSON は [line, [hits...]] を読む。
func (e *PauseLineEntry) UnmarshalJSON(b []byte) error { return unmarshalTuple(b, &e.Line, &e.Hits) }

// PauseFile は入力の集まりの 1 ファイルの記録。Lines は trigger の字 (は、が、も、を、など) を含む行だけ。Table と TableObjects は
// analyze_markdown(decode_source(data)[0]) を _pause_comma_line_hits に objects=False と True で通した表。
type PauseFile struct {
	Path         string         `json:"path"`
	SHA256       string         `json:"sha256"`
	Lines        []PauseStrCase `json:"lines"`
	Table        PauseLineTable `json:"table"`
	TableObjects PauseLineTable `json:"table_objects"`
}

// PauseDoc は手で作った Markdown の文書と、その表 (段落をまたぐ文・箇条書き・見出しのような行・引用・かなで終わる段落の例)。
type PauseDoc struct {
	Text         WStr           `json:"text"`
	Table        PauseLineTable `json:"table"`
	TableObjects PauseLineTable `json:"table_objects"`
}

// PauseCommaGolden は pause_comma.json の中身。Texts と Docs は手で作った入力 (PauseCommaTexts・PauseCommaDocs)。
type PauseCommaGolden struct {
	Header Header         `json:"header"`
	Texts  []PauseStrCase `json:"texts"`
	Docs   []PauseDoc     `json:"docs"`
	Files  []PauseFile    `json:"files"`
}

// MarshalPauseComma は PauseCommaGolden を決まったバイト列にする (1 行 1 要素)。
func MarshalPauseComma(g PauseCommaGolden) ([]byte, error) {
	header, err := marshalField("header", g.Header)
	if err != nil {
		return nil, err
	}
	texts, err := marshalLines(g.Texts)
	if err != nil {
		return nil, err
	}
	docs, err := marshalLines(g.Docs)
	if err != nil {
		return nil, err
	}
	files, err := marshalLines(g.Files)
	if err != nil {
		return nil, err
	}
	return []byte("{\n" + header + ",\n\"texts\": " + texts + ",\n\"docs\": " + docs + ",\n\"files\": " + files + "\n}\n"), nil
}

// RecordPauseComma は入力の集まりの全ファイルと PauseCommaTexts を本家の Python に通して記録にする。
func (r Runner) RecordPauseComma(ctx context.Context, header Header, c Corpus) (PauseCommaGolden, error) {
	sb, err := r.newStagedSandbox()
	if err != nil {
		return PauseCommaGolden{}, err
	}
	defer sb.remove()
	type fileIn struct {
		Path string `json:"path"`
		Data string `json:"data"`
	}
	in := struct {
		ScriptsDir string   `json:"scripts_dir"`
		Texts      []WStr   `json:"texts"`
		Docs       []WStr   `json:"docs"`
		Files      []fileIn `json:"files"`
	}{ScriptsDir: sb.tree.Path(ScriptsDir)}
	for _, t := range PauseCommaTexts {
		in.Texts = append(in.Texts, WStr(t))
	}
	for _, d := range PauseCommaDocs {
		in.Docs = append(in.Docs, WStr(d))
	}
	for _, f := range c.Files {
		body, err := os.ReadFile(filepath.Join(c.Root, filepath.FromSlash(f.Path)))
		if err != nil {
			return PauseCommaGolden{}, fmt.Errorf("入力 %s を読めません: %w", f.Path, err)
		}
		in.Files = append(in.Files, fileIn{Path: f.Path, Data: string(body)})
	}
	var out struct {
		Texts []PauseStrCase `json:"texts"`
		Docs  []PauseDoc     `json:"docs"`
		Files []PauseFile    `json:"files"`
	}
	if err := r.runDriver(ctx, sb, "record_pause_comma.py", in, &out); err != nil {
		return PauseCommaGolden{}, err
	}
	if len(out.Texts) != len(PauseCommaTexts) || len(out.Docs) != len(PauseCommaDocs) || len(out.Files) != len(c.Files) {
		return PauseCommaGolden{}, fmt.Errorf("pause_comma の入力 (文 %d・ファイル %d) に対して結果が (%d・%d) でした", len(PauseCommaTexts), len(c.Files), len(out.Texts), len(out.Files))
	}
	for k := range out.Files {
		out.Files[k].SHA256 = c.Files[k].SHA256
	}
	if len(out.Texts) == 0 {
		return PauseCommaGolden{}, errors.New("pause_comma の手で作った入力が 0 件です")
	}
	return PauseCommaGolden{Header: header, Texts: out.Texts, Docs: out.Docs, Files: out.Files}, nil
}
