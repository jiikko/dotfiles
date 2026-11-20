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
	// Args は subcommand 名より後ろの引数 (プログラム名を含まない)。字は本家の sys.argv の str と同じ (WTF-8。FromProcess は os.fsdecode と同じ規則で読む)。
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Cwd は作業フォルダ。字は本家の os.getcwd() と同じ (FromProcess は os.fsdecode と同じ規則で読む)。
	Cwd string
	// Environ は環境変数の全部 ("名前=値" の並び)。Lookup・Getenv・Environment はここだけから読む。
	// 読み口を 1 つにして、手で組んだ Env の間で「ある」の答えが食い違わないようにする (本家の os.environ と同じく、空の値は「ある」)。
	// 字は本家の os.environ の str と同じ (WTF-8)。FromProcess は OS のバイトを pyfs.Decode (os.fsdecode) で読む。
	// 生のバイトのまま持たない: WTF-8 のサロゲートの形 (ED A0..BF xx) が単独のサロゲートと見分けられず、子に渡すとき書き換わるか断られる。
	// nil は環境変数が 1 つも無い環境 (プロセスの環境を引き継がない)。
	Environ []string
}

// Lookup は環境変数 name を読む (os.environ.get の形。空の値と無いを分ける)。同じ名前が 2 つあれば先のもの (os.environ と os.Getenv と同じ)。
// 値は本家の os.environ と同じ字 (Environ の字そのまま)。パスとして OS に渡す読み手は internal/pyos を通して os.fsencode したバイトにする。
func (e Env) Lookup(name string) (string, bool) {
	for _, entry := range e.Environ {
		if key, value, ok := strings.Cut(entry, "="); ok && key == name {
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

// FromProcess は今のプロセスの引数・標準入出力・cwd・環境変数から Env を作る。引数・cwd・環境変数は os.fsdecode と同じ規則 (pyfs.Decode) で
// 本家の str と同じ字にする。生のバイトのまま持たない: UTF-8 として読めないバイト (0xff) は記録の JSON と誤りの文で本家の "\udcff" と別の字になり、
// WTF-8 のサロゲートの形 (ED A0..BF xx) は単独のサロゲートと見分けられず、子に渡すとき書き換わるか断られる。
// OS に渡すときは internal/pyos が os.fsencode と同じバイトに戻す。
func FromProcess(args []string) (Env, error) {
	cwd, err := os.Getwd() //nolint:forbidigo // 受けた字は下で pyfs.Decode する。os.Getwd ($PWD を使う) を syscall.Getwd に替えると Cwd の値が変わるので、この口だけ os を直に呼ぶ
	if err != nil {
		return Env{}, fmt.Errorf("cwd を読めません: %w", err)
	}
	decoded := make([]string, len(args))
	for i, a := range args {
		decoded[i] = pyfs.Decode(a)
	}
	return Env{Args: decoded, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Cwd: pyfs.Decode(cwd), Environ: processEnviron()}, nil
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
