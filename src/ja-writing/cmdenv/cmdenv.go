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
)

// Env は 1 回の subcommand の実行が見る外の世界。
type Env struct {
	// Args は subcommand 名より後ろの引数 (プログラム名を含まない)。
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Cwd    string
	// Environ は環境変数の全部 ("名前=値" の並び。os.Environ の形)。Lookup・Getenv・Environment はここだけから読む。
	// 読み口を 1 つにして、手で組んだ Env の間で「ある」の答えが食い違わないようにする (本家の os.environ と同じく、空の値は「ある」)。
	// nil は環境変数が 1 つも無い環境 (プロセスの環境を引き継がない)。
	Environ []string
}

// Lookup は環境変数 name を読む (os.LookupEnv の形。空の値と無いを分ける)。同じ名前が 2 つあれば先のもの (os.environ と os.Getenv と同じ)。
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

// Environment は環境変数の全部の写し (nil にしない: exec.Cmd の Env の nil は今のプロセスの環境を引き継ぐ)。
func (e Env) Environment() []string {
	return append([]string{}, e.Environ...)
}

// FromProcess は今のプロセスの引数・標準入出力・cwd・環境変数から Env を作る。
func FromProcess(args []string) (Env, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Env{}, fmt.Errorf("cwd を読めません: %w", err)
	}
	return Env{Args: args, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Environ: os.Environ()}, nil
}

// ExitUsage は本家の argparse と同じく、使い方か入力の誤りを表す終了コード。
const ExitUsage = 2

// Unimplemented は、まだ Go で書いていない subcommand が返す終了コード (ExitUsage) を返し、その旨を標準エラーへ出す。
func Unimplemented(env Env, name string) int {
	// 標準エラーへの書き込みの失敗は、終了コードを変えられないので捨てる。
	_, _ = fmt.Fprintf(env.Stderr, "ja-writing %s: まだ実装していません\n", name)
	return ExitUsage
}
