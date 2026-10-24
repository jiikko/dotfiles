// Package report は重複の報告を、人が読む形・JSON・Xcode / SwiftLint と同じ形式で書く。
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"dupscan/internal/group"
	"dupscan/internal/tree"
)

// Summary は走査の数え上げ。
type Summary struct {
	Root            string      `json:"root"`
	Files           int         `json:"files"`
	Units           int         `json:"units"`
	FilesWithErrors []string    `json:"files_with_errors"`
	Unreadable      []UnitRef   `json:"unreadable_units"`
	Groups          []GroupJSON `json:"groups"`
}

// UnitRef は単位 1 つの出力。
type UnitRef struct {
	Key        string `json:"key"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Col        int    `json:"col"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Signature  string `json:"signature,omitempty"`
	Enclosing  string `json:"enclosing,omitempty"`
	Test       bool   `json:"test"`
	Visibility string `json:"visibility"`
}

// GroupJSON は組 1 つの出力。
type GroupJSON struct {
	Level     string    `json:"level"`
	Hash      string    `json:"hash"`
	Partition string    `json:"partition"`
	Members   []UnitRef `json:"members"`
}

// Build は出力の形を組み立てる。
func Build(root string, files int, units []*tree.Unit, errFiles []string, groups []group.Group) Summary {
	s := Summary{Root: root, Files: files, Units: len(units), FilesWithErrors: errFiles}
	if s.FilesWithErrors == nil {
		s.FilesWithErrors = []string{}
	}
	s.Unreadable = []UnitRef{}
	for _, u := range units {
		if u.Unreadable {
			s.Unreadable = append(s.Unreadable, ref(u))
		}
	}
	s.Groups = []GroupJSON{}
	for _, g := range groups {
		gj := GroupJSON{Level: g.Level.String(), Hash: g.Hash, Partition: g.Partition}
		for _, m := range g.Members {
			gj.Members = append(gj.Members, ref(m.Unit))
		}
		s.Groups = append(s.Groups, gj)
	}
	return s
}

func ref(u *tree.Unit) UnitRef {
	return UnitRef{Key: u.Key, File: u.File, Line: u.Line, Col: u.Col, Kind: string(u.Kind), Name: u.Name,
		Signature: u.Signature, Enclosing: u.Enclosing, Test: u.Test, Visibility: u.Visibility}
}

// JSON は JSON で書く。
func JSON(w io.Writer, s Summary) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(s)
}

// TextOptions は人が読む形の設定。
type TextOptions struct {
	// Limit は出す組の上限 (0 は全部)。Claude が何度も回すので、既定は短くする。
	Limit int
	// Verbose は読めない構文の単位を 1 つずつ出す。
	Verbose bool
}

// Text は人が読む形で書く。重複が無いときは 1 行だけにする (何度も回す Claude の文脈を食わないように)。
func Text(w io.Writer, s Summary, opt TextOptions) error {
	if len(s.Groups) == 0 {
		fmt.Fprintf(w, "dupscan: 重複なし (%d ファイル・%d 単位)%s\n", s.Files, s.Units, unreadableNote(s))
		return nil
	}
	fmt.Fprintf(w, "dupscan: 組 %d (%s)・%d ファイル・%d 単位%s\n", len(s.Groups), levelCounts(s), s.Files, s.Units, unreadableNote(s))
	shown := s.Groups
	if opt.Limit > 0 && len(shown) > opt.Limit {
		shown = shown[:opt.Limit]
	}
	for _, g := range shown {
		fmt.Fprintf(w, "%s %s:\n", g.Level, g.Partition)
		for _, m := range g.Members {
			fmt.Fprintf(w, "  %s:%d %s\n", m.File, m.Line, label(m))
		}
	}
	if rest := len(s.Groups) - len(shown); rest > 0 {
		fmt.Fprintf(w, "… 他 %d 組 (-limit 0 か -format json で全部)\n", rest)
	}
	if opt.Verbose {
		for _, u := range s.Unreadable {
			fmt.Fprintf(w, "読めない: %s:%d %s\n", u.File, u.Line, label(u))
		}
	}
	return nil
}

func levelCounts(s Summary) string {
	n := map[string]int{}
	var order []string
	for _, g := range s.Groups {
		if n[g.Level] == 0 {
			order = append(order, g.Level)
		}
		n[g.Level]++
	}
	parts := make([]string, len(order))
	for i, l := range order {
		parts[i] = fmt.Sprintf("%s %d", l, n[l])
	}
	return strings.Join(parts, "・")
}

func unreadableNote(s Summary) string {
	if len(s.Unreadable) == 0 {
		return ""
	}
	return fmt.Sprintf("・読めない構文で比べなかった単位 %d (-v で一覧)", len(s.Unreadable))
}

// Xcode は `ファイル:行:列: warning: …` の形式で書く (swiftlint と並べて回し、Xcode の警告として出すため)。
// 組の先頭のメンバーを warning の 1 行にし、残りのメンバーを note の行で続ける。パスは絶対パス (Xcode が飛べるように)。
func Xcode(w io.Writer, s Summary) error {
	for _, g := range s.Groups {
		first := g.Members[0]
		fmt.Fprintf(w, "%s:%d:%d: warning: %s は他の %d 個と本体が同じ形 (dupscan.%s)\n",
			filepath.Join(s.Root, first.File), first.Line, first.Col, label(first), len(g.Members)-1, g.Level)
		for _, m := range g.Members[1:] {
			fmt.Fprintf(w, "%s:%d:%d: note: 同じ形: %s (dupscan.%s)\n", filepath.Join(s.Root, m.File), m.Line, m.Col, label(m), g.Level)
		}
	}
	for _, u := range s.Unreadable {
		fmt.Fprintf(w, "%s:%d:%d: note: 読めない構文があるので比べなかった: %s (dupscan)\n", filepath.Join(s.Root, u.File), u.Line, u.Col, label(u))
	}
	return nil
}

func label(u UnitRef) string {
	name := u.Signature
	if name == "" {
		name = u.Name
	}
	if u.Enclosing != "" {
		return u.Enclosing + "." + name
	}
	return name
}
