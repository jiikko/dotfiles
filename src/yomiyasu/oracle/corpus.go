package oracle

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CorpusFile は入力の集まりの 1 ファイル。Lines は "\n" で割った行の数 (末尾の改行の後の空の行も数える。空のファイルは 1)。
type CorpusFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Lines  int    `json:"lines"`
}

// Corpus は testdata/corpus/ 以下の入力の集まり。Files は Path の昇順 (バイトの順) で、記録も Python への指示もこの順で扱う。
type Corpus struct {
	Root  string
	Files []CorpusFile
}

// LoadCorpus は root 以下の通常のファイルを全部読み、UTF-8 として読めることを確かめる。0 件はエラー (空振りで緑にしない)。
func LoadCorpus(root string) (Corpus, error) {
	var files []CorpusFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if !utf8.Valid(body) {
			return fmt.Errorf("%s が UTF-8 として読めません", p)
		}
		files = append(files, CorpusFile{Path: filepath.ToSlash(rel), SHA256: SHA256Hex(body), Lines: strings.Count(string(body), "\n") + 1})
		return nil
	})
	if err != nil {
		return Corpus{}, fmt.Errorf("入力の集まり %s を読めません: %w", root, err)
	}
	if len(files) == 0 {
		return Corpus{}, fmt.Errorf("入力の集まり %s に 1 件もありません", root)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return Corpus{Root: root, Files: files}, nil
}

// Units は記録の単位の数 (ファイルごとに全文 1 つと行ごとに 1 つ)。
func (c Corpus) Units() int {
	n := 0
	for _, f := range c.Files {
		n += 1 + f.Lines
	}
	return n
}

// Read はファイルの中身を str として返す。改行は変換しない (CRLF の "\r" は行の末尾に残る)。
func (c Corpus) Read(rel string) (string, error) {
	body, err := os.ReadFile(filepath.Join(c.Root, filepath.FromSlash(rel)))
	if err != nil {
		return "", fmt.Errorf("入力 %s を読めません: %w", rel, err)
	}
	return string(body), nil
}

// UnitKey は記録の単位の名前。line < 0 は全文 ("<path>#doc")、0 以上は "\n" で割った line 番目の行 ("<path>#<line>")。
func UnitKey(path string, line int) string {
	if line < 0 {
		return path + "#doc"
	}
	return path + "#" + strconv.Itoa(line)
}

// readDirFiles は dir の直下の通常のファイルを名前 → 中身で返す (語の一覧のフォルダ用)。
func readDirFiles(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%s を読めません: %w", dir, err)
	}
	files := map[string]string{}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		body, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return nil, fmt.Errorf("%s を読めません: %w", e.Name(), rerr)
		}
		files[e.Name()] = string(body)
	}
	return files, nil
}
