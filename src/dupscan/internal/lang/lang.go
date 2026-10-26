// Package lang は、言語ごとの adapter が満たす形と、adapter がコマンドへ返す結果の型を持つ。
//
// adapter はファイルを読んで、比べる単位 (internal/tree の Unit) と、索引・呼び出し元・定義・参照の材料を返す。
// コマンドはどの adapter が読んだかを知らずに、ここの型だけを扱う (adapter の選び方は internal/langs)。
package lang

import "dupscan/internal/tree"

// Options は単位のほかに何を集めるか。集めない材料は読むたびの確保と時間を食うので、使うコマンドだけが頼む。
type Options struct {
	Calls bool // 呼び出しの箇所 (index・callers)
	Defs  bool // 定義と型の宣言 (def)
	Refs  bool // 識別子の出現 (refs)
}

// Parser は 1 つの goroutine の中で使う読み手 (並行に使わない。worker ごとに作る)。
type Parser interface {
	Parse(file string, src []byte) (FileResult, error)
	Close()
}

// Adapter は 1 つの言語の読み方。
type Adapter struct {
	Name       string
	Extensions []string
	// IsTestPath はその言語の慣習で test のファイルかを決める。
	IsTestPath func(file string) bool
	NewParser  func(Options) (Parser, error)
	// Symbols は def と refs の材料 (Options.Defs / Refs) を集めるか。集めない言語のファイルを def / refs は読まない。
	Symbols bool
}

// FileResult は 1 ファイルの結果。
type FileResult struct {
	Units       []*tree.Unit
	Calls       []tree.Call
	Definitions []Definition
	Types       []TypeDeclaration
	References  []Reference
	// HasError はファイルのどこかに読めない構文があること。読めない部分に飲み込まれた関数は単位として出てこないので、
	// 呼び出し側は「比べていない部分がある」ことを出力に出す。
	HasError bool
}

// Definition は def 用の定義情報。重複を比べる Unit ではない。
type Definition struct {
	Name       string
	Kind       string
	File       string
	Line       int
	EndLine    int
	Col        int
	Enclosing  string
	Signature  string
	Labels     []string
	Test       bool
	Nested     bool
	Visibility string
	Doc        string
	// DocStart / DocEnd は doc comment の行の範囲 (無ければ 0)。evidence が doc の後半の語から定義へ辿る
	DocStart, DocEnd int
}

// TypeDeclaration は型本体の宣言情報。Unit には含めない。
type TypeDeclaration struct {
	Name       string
	Kind       string
	File       string
	Line       int
	EndLine    int
	Col        int
	Enclosing  string
	Scope      string
	Signature  string
	Test       bool
	Nested     bool
	Visibility string
	Doc        string
	DocStart   int // Definition と同じ
	DocEnd     int
}

// Reference は refs 用の識別子の出現。Kind は type_position または expression_position。
type Reference struct {
	Name      string
	Kind      string
	File      string
	Line      int
	Col       int
	Text      string
	Test      bool
	Enclosing string
	TestOwner string
}
