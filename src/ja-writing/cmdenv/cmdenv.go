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
}

// FromProcess は今のプロセスの引数・標準入出力・cwd・環境変数から Env を作る。
func FromProcess(args []string) (Env, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Env{}, fmt.Errorf("cwd を読めません: %w", err)
	}
	return Env{Args: args, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Getenv: os.Getenv}, nil
}

// ExitUsage は本家の argparse と同じく、使い方か入力の誤りを表す終了コード。
const ExitUsage = 2

// Unimplemented は、まだ Go で書いていない subcommand が返す終了コード (ExitUsage) を返し、その旨を標準エラーへ出す。
func Unimplemented(env Env, name string) int {
	// 標準エラーへの書き込みの失敗は、終了コードを変えられないので捨てる。
	_, _ = fmt.Fprintf(env.Stderr, "ja-writing %s: まだ実装していません\n", name)
	return ExitUsage
}
