// Package cmdenv は ja-writing の各 subcommand の Main が受け取る実行環境 (引数・標準入出力・cwd・環境変数) を持つ。
//
// subcommand を os.Args・os.Stdout・os.Getwd から切り離すのは、check が lint と diff を子プロセスで動かすか関数で呼ぶかを
// 後から選んでも Main の形が変わらないようにするため。テストも同じ口で標準入出力と cwd を差し替えられる。
package cmdenv

import (
	"fmt"
	"io"
	"os"
)

// Env は 1 回の subcommand の実行が見る外の世界。
type Env struct {
	// Args は subcommand 名より後ろの引数 (プログラム名を含まない)。
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Cwd    string
	// Getenv は環境変数の読み出し。os.Getenv を直接呼ばず、テストが差し替えられるようにする。
	Getenv func(string) string
	// LookupEnv は空と無いを分けて読む環境変数 (os.LookupEnv の形。HOME が空のときと無いときで扱いが違う所が使う)。
	// nil なら Getenv で読み、空を無いとみなす (Lookup)。
	LookupEnv func(string) (string, bool)
	// Environ は環境変数の全部 (os.Environ の形。check が子の環境を組む)。nil なら空 (Environment)。
	Environ func() []string
}

// Lookup は環境変数 name を読む。LookupEnv が無ければ Getenv で読み、空の値を無いとみなす。
func (e Env) Lookup(name string) (string, bool) {
	if e.LookupEnv != nil {
		return e.LookupEnv(name)
	}
	if e.Getenv == nil {
		return "", false
	}
	value := e.Getenv(name)
	return value, value != ""
}

// Environment は環境変数の全部。Environ が無ければ空 (nil にしない: exec.Cmd の Env の nil は今のプロセスの環境を引き継ぐ)。
func (e Env) Environment() []string {
	if e.Environ == nil {
		return []string{}
	}
	return e.Environ()
}

// FromProcess は今のプロセスの引数・標準入出力・cwd・環境変数から Env を作る。
func FromProcess(args []string) (Env, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Env{}, fmt.Errorf("cwd を読めません: %w", err)
	}
	return Env{Args: args, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Getenv: os.Getenv, LookupEnv: os.LookupEnv, Environ: os.Environ}, nil
}

// ExitUsage は本家の argparse と同じく、使い方か入力の誤りを表す終了コード。
const ExitUsage = 2

// Unimplemented は、まだ Go で書いていない subcommand が返す終了コード (ExitUsage) を返し、その旨を標準エラーへ出す。
func Unimplemented(env Env, name string) int {
	// 標準エラーへの書き込みの失敗は、終了コードを変えられないので捨てる。
	_, _ = fmt.Fprintf(env.Stderr, "ja-writing %s: まだ実装していません\n", name)
	return ExitUsage
}
