package oracle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiffCLIFile は本家 diff の CLI の単位の記録 (testdata/golden/ の中)。形は lint-cli.json と同じ (stdout は SHA-256 と長さ、stderr は全文、rc。--json の出力は何が出たか (kinds) も持つ)。
const DiffCLIFile = "diff-cli.json"

// DiffInput は testdata/diff/<名前>/ の直す前と後の組 (old.md・new.md) と、--material で渡す資料 (material.md か material1.md から)。
// 手で作った入力で、本家の入力の集まり (testdata/corpus/) とは別の置き場にする: corpus に足すと、corpus を入力にする全部の記録が作り直しになる。
type DiffInput struct {
	Name      string
	Materials []string
}

// LoadDiffInputs は root 直下の dir を名前の昇順で返す。1 件もなければ error (空振りで緑にしない)。
func LoadDiffInputs(root string) ([]DiffInput, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("diff の入力 %s を読めません: %w", root, err)
	}
	var inputs []DiffInput
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		in := DiffInput{Name: e.Name()}
		files, err := os.ReadDir(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			switch name := f.Name(); {
			case name == "old.md" || name == "new.md":
			case strings.HasPrefix(name, "material") && strings.HasSuffix(name, ".md"):
				in.Materials = append(in.Materials, name)
			default:
				return nil, fmt.Errorf("%s/%s は diff の入力の名前ではありません (old.md・new.md・material*.md)", e.Name(), name)
			}
		}
		sort.Strings(in.Materials)
		inputs = append(inputs, in)
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("diff の入力 %s に 1 件もありません", root)
	}
	return inputs, nil
}

// DiffInputPaths は作業フォルダの中の入力の置き場 (diff/<名前>/)。
func DiffInputPaths(name string) (oldPath, newPath string) {
	return "diff/" + name + "/old.md", "diff/" + name + "/new.md"
}

// DiffCorpusPairs は入力の集まりの upstream/ から、直す前と後の組 (本家の raw_ai の文書と、同じ名前の yomiyasu_rewritten・blacklist_ai、
// と yomiyasu_rewritten の _bl の付いた名前)。組の名前は「<raw の名前>~<後の置き場>」。
func DiffCorpusPairs(c Corpus) [][3]string {
	has := map[string]bool{}
	for _, f := range c.Files {
		has[f.Path] = true
	}
	var pairs [][3]string
	for _, f := range c.Files {
		name, ok := strings.CutPrefix(f.Path, "upstream/raw_ai/")
		if !ok {
			continue
		}
		stem := strings.TrimSuffix(name, ".md")
		for _, to := range []string{"yomiyasu_rewritten/" + name, "blacklist_ai/" + name, "yomiyasu_rewritten/" + stem + "_bl.md"} {
			if has["upstream/"+to] {
				pairs = append(pairs, [3]string{strings.TrimSuffix(name, ".md") + "~" + strings.ReplaceAll(strings.TrimSuffix(to, ".md"), "/", ":"), "corpus/" + f.Path, "corpus/upstream/" + to})
			}
		}
	}
	return pairs
}

// DiffCLIFiles は CLI の記録の作業フォルダに置くファイル (lint の記録のファイルに、diff の入力を足したもの)。
func DiffCLIFiles(c Corpus, wordsDir, diffDir string) (map[string]string, error) {
	files, err := LintCLIFiles(c, wordsDir)
	if err != nil {
		return nil, err
	}
	files["withwords/a.md"] = "# まとめ\n\n設定を扱います。ポイントは3つです。\n"
	files["withwords/b.md"] = "# まとめ\n\n設定を扱います。ポイントは4つです。サーバを使います。\n"
	inputs, err := LoadDiffInputs(diffDir)
	if err != nil {
		return nil, err
	}
	for _, in := range inputs {
		for _, name := range append([]string{"old.md", "new.md"}, in.Materials...) {
			body, err := os.ReadFile(filepath.Join(diffDir, in.Name, name))
			if err != nil {
				return nil, err
			}
			files["diff/"+in.Name+"/"+name] = string(body)
		}
	}
	return files, nil
}

// RecordDiffCLI は cases を、files を置いた作業フォルダで本家 diff の CLI に通して記録にする。
func (r Runner) RecordDiffCLI(ctx context.Context, header Header, cases []LintCLICase, files map[string]string) (LintCLIGolden, error) {
	return r.recordCLI(ctx, "record_diff_cli.py", header, cases, files)
}
