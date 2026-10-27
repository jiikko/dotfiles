package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"dupscan/internal/jsonout"
	"dupscan/internal/langs"
)

func runAgentGuide(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dupscan agent-guide", flag.ContinueOnError)
	fs.SetOutput(stderr)
	target := fs.String("target", "", "案内するagent: claude / codex")
	language := fs.String("lang", "all", "対象: all / swift / go")
	printOnly := fs.Bool("print", false, "案内を表示する (設定を書き換えない)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(message string) int { fmt.Fprintln(stderr, "dupscan agent-guide:", message); return 2 }
	if *target != "claude" && *target != "codex" {
		return fail("-target claude または codex を指定する")
	}
	if !*printOnly || len(fs.Args()) != 0 {
		return fail("-print で表示する。位置引数や設定の書き込みには対応しない")
	}
	symbols := false
	supported := *language == "all"
	for _, adapter := range langs.All {
		if *language == "all" || adapter.Name == *language {
			supported = true
			symbols = symbols || adapter.Symbols
		}
	}
	if !supported {
		return fail("-lang は all または登録済みの言語名")
	}
	actions := []nextAction{
		{Kind: "index", Reason: "書く前に戻り値の型で既存helperを探す", Argv: []string{"dupscan", "index", "-returns", "TYPE"}},
		{Kind: "evidence", Reason: "名前不明ならdoc全文・テスト・ソースの語から候補を探す", Argv: []string{"dupscan", "evidence", "TERM"}},
	}
	if symbols {
		actions = append(actions, nextAction{Kind: "context", Reason: "Swiftの定義・参照・呼び出しを確認する", Argv: []string{"dupscan", "context", "SYMBOL"}})
	} else {
		actions = append(actions, nextAction{Kind: "callers", Reason: "Goの呼び出し元を名前で近似して調べる", Argv: []string{"dupscan", "callers", "SYMBOL"}})
	}
	actions = append(actions, nextAction{Kind: "gate", Reason: "実装後に増えた写しを確かめる", Argv: []string{"dupscan", "gate"}})
	var out bytes.Buffer
	fmt.Fprintf(&out, "dupscan agent-guide: %s / %s\n", *target, *language)
	fmt.Fprintln(&out, "TYPE / TERM / SYMBOLを実際の型・用途語・名前へ置き換え、対象repoのcwdから使う。必要なら末尾にdirを指定する。")
	fmt.Fprintln(&out, "0件は不存在の証明ではない。構文解析の失敗・scope・未対応言語を確認し、文字列/設定はrgやReadを使う。型解決はせず、候補の所属/再利用可否はコードを読んで判断する。SDK側の定義はLSP等で確認する。")
	if *language == "all" {
		fmt.Fprintln(&out, "context/def/refsはSwiftだけ。Goの定義・参照はLSP等で確認し、callers/indexはGoにも使える。")
	}
	if *target == "claude" {
		fmt.Fprintln(&out, "Claude: 共通CLIをshellから使う。既存grep案内hookはClaude向けで、通知だけで解析/buildを起こさない。")
	} else {
		fmt.Fprintln(&out, "Codex: 共通CLIをshellから使い、必要ならrepository instructionsへ利用手順を記載する。Claude固有hookがCodexで動くとは仮定しない。")
	}
	for _, action := range actions {
		registered := false
		for _, spec := range commandSpecs() {
			if spec.Name == action.Kind && (!spec.SymbolsOnly || symbols) {
				registered = true
				break
			}
		}
		if !registered {
			continue
		}
		encoded, err := jsonout.Line(action.Argv)
		if err != nil {
			return fail(err.Error())
		}
		fmt.Fprintf(&out, "%s\nargv: %s\n", action.Reason, encoded)
	}
	fmt.Fprintln(&out, "初回buildは通常起動で行う。通知等でbuildを避けるには先頭の-no-buildを使い、準備不足のrc=2で自動再試行しない。案内は自動実行せず、設定も変更しない。")
	if out.Len() > 4096 {
		return fail("案内が4096 bytesを超えた")
	}
	if _, err := io.Copy(stdout, strings.NewReader(out.String())); err != nil {
		return fail(err.Error())
	}
	return 0
}
