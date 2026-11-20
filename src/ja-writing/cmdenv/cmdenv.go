// Package cmdenv は ja-writing の各 subcommand の Main が受け取る実行環境 (引数・標準入出力・cwd・環境変数) を持つ。
//
// subcommand を os.Args・os.Stdout・os.Getwd から切り離すのは、check が lint と diff を子プロセスで動かすか関数で呼ぶかを
// 後から選んでも Main の形が変わらないようにするため。テストも同じ口で標準入出力と cwd を差し替えられる。
package cmdenv

import (
	"fmt"
	"io"
	"os"
	"strings"

	"ja-writing/internal/pyfs"
)

// Env は 1 回の subcommand の実行が見る外の世界。
type Env struct {
	// Args は subcommand 名より後ろの引数 (プログラム名を含まない)。
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Cwd    string
	// Environ は環境変数の全部 ("名前=値" の並び)。Lookup・Getenv・Environment はここだけから読む。
	// 読み口を 1 つにして、手で組んだ Env の間で「ある」の答えが食い違わないようにする (本家の os.environ と同じく、空の値は「ある」)。
	// 字は本家の os.environ の str と同じ (WTF-8)。FromProcess は OS のバイトを pyfs.Decode (os.fsdecode) で読む。
	// 生のバイトのまま持たない: WTF-8 のサロゲートの形 (ED A0..BF xx) が単独のサロゲートと見分けられず、子に渡すとき書き換わるか断られる。
	// nil は環境変数が 1 つも無い環境 (プロセスの環境を引き継がない)。
	Environ []string
}

// Lookup は環境変数 name を読む (os.LookupEnv の形。空の値と無いを分ける)。同じ名前が 2 つあれば先のもの (os.environ と os.Getenv と同じ)。
// 値は os.fsencode したバイト (OS から受けた値なら受けたバイトそのもの)。読み手は値をパス (HOME・TMPDIR・YOMIYASU_STATE_DIR) として
// OS に渡すので、本家が open などの前に os.fsencode するのと同じバイトにする。戻せないサロゲート (手で組んだ値の U+D800 など) を持つ値は
// 字のまま返す (本家はその値を OS に渡す所で UnicodeEncodeError にする。Go の読み手はその誤りを写していない)。
func (e Env) Lookup(name string) (string, bool) {
	for _, entry := range e.Environ {
		if key, value, ok := strings.Cut(entry, "="); ok && key == name {
			if encoded, err := pyfs.Encode(value); err == nil {
				return encoded, true
			}
			return value, true
		}
	}
	return "", false
}

// Getenv は環境変数 name の値 (無ければ空)。
func (e Env) Getenv(name string) string {
	value, _ := e.Lookup(name)
	return value
}

// Environment は子に渡す環境変数の全部 (nil にしない: exec.Cmd の Env の nil は今のプロセスの環境を引き継ぐ)。字は Environ と同じ str の形で、
// 子を起こす所 (check の popenEncode) が os.fsencode と同じバイトにする。
// 同じ名前が 2 つあれば先のものだけを残し、"=" の無いものは落とす (Lookup が読むものと同じ)。重なりのまま渡すと、Go の exec は
// 後のものを残すので、親の Lookup と子が別の値を読む。本家は os.environ (先のもの) から dict を作って子に渡す。
func (e Env) Environment() []string {
	kept := []string{}
	seen := map[string]bool{}
	for _, entry := range e.Environ {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, entry)
	}
	return kept
}

// FromProcess は今のプロセスの引数・標準入出力・cwd・環境変数から Env を作る。環境変数は os.fsdecode と同じ規則で読む (Environ)。
// 引数と cwd は OS のバイトのまま持つ: どちらも子にそのまま渡らず (子の作業フォルダは一時フォルダ)、読み手はパスとして OS に渡すだけなので、
// 読むと OS に渡す所ごとに戻す手間が増える。子の argv に入る引数のうち --stance・--doc-type・--reader は決まった字に写してから渡り、パス (語の一覧など) は
// macOS ではサロゲートの形 (ED A0..BF xx) を持てない。
func FromProcess(args []string) (Env, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Env{}, fmt.Errorf("cwd を読めません: %w", err)
	}
	return Env{Args: args, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Environ: processEnviron()}, nil
}

// processEnviron は os.Environ を os.fsdecode と同じ規則で読んだもの (Environ の形)。
func processEnviron() []string {
	environ := os.Environ()
	for i, entry := range environ {
		environ[i] = pyfs.Decode(entry)
	}
	return environ
}

// ExitUsage は本家の argparse と同じく、使い方か入力の誤りを表す終了コード。
const ExitUsage = 2

// Unimplemented は、まだ Go で書いていない subcommand が返す終了コード (ExitUsage) を返し、その旨を標準エラーへ出す。
func Unimplemented(env Env, name string) int {
	// 標準エラーへの書き込みの失敗は、終了コードを変えられないので捨てる。
	_, _ = fmt.Fprintf(env.Stderr, "ja-writing %s: まだ実装していません\n", name)
	return ExitUsage
}
