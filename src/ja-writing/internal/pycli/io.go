package pycli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"ja-writing/cmdenv"
	"ja-writing/visibility"
)

// 終了コード (本家の argparse の定義): 1 は --fail-on などの条件に当たった・3 は内部の誤りか出力先が閉じた。2 (使い方か入力の誤り) は cmdenv.ExitUsage。
const (
	ExitHit      = 1
	ExitInternal = 3
	// ExitLateFlush は Python が終わり際に標準出力・標準エラーを書き出せなかったときの終了コード (Py_FinalizeEx の失敗で 120)。
	ExitLateFlush = 120
)

// ErrOutputClosed は読む側が閉じた出力先に書いたこと (EPIPE。本家の BrokenPipeError)。
var ErrOutputClosed = errors.New("output closed")

// LateFlushError は、本家では書き込みが終わり際の flush まで遅れる出力 (-h・--version・使い方の誤り。どれも本家は自分で flush せずに終わる) が、
// 読む側の閉じたパイプで書けなかったこと。本家はこのとき main の BrokenPipeError を通らず、インタプリタが rc 120 で終わる (stdout なら 2 行の知らせを出す)。
type LateFlushError struct{ Stream string }

func (e LateFlushError) Error() string { return e.Stream + " の書き出しの失敗 (終わり際)" }

// AtExit は、本家で終わり際の flush まで遅れる書き込みの誤りを LateFlushError にする。EPIPE 以外 (ENOSPC など) はそのまま返す。
func AtExit(stream string, err error) error {
	if errors.Is(err, ErrOutputClosed) {
		return LateFlushError{stream}
	}
	return err
}

// Eprintln は標準エラーに 1 行書く (書けなくても何もしない。本家の _error)。
func Eprintln(env cmdenv.Env, message string) { _, _ = io.WriteString(env.Stderr, message+"\n") }

// PrintTo は標準出力に書く。書けなかった誤りは WriteTo で分ける。
func PrintTo(env cmdenv.Env, text string) error { return WriteTo(env.Stdout, text) }

// UsageTo は parser.error の文を標準エラーに書く。書けなかったら、本家と同じく rc 2 でなく終わり際の失敗 (LateFlushError) にする
// (本家の _error が書けなくても黙って rc 2 を返すのとは別)。
func UsageTo(env cmdenv.Env, usage, prog, message string) (int, error) {
	if err := WriteTo(env.Stderr, UsageError(usage, prog, message)); err != nil {
		return 0, AtExit("stderr", err)
	}
	return cmdenv.ExitUsage, nil
}

// WriteTo は w に書き、書けなかった誤りを本家の main の分け方で返す: 読む側が閉じた (EPIPE。本家の BrokenPipeError) は ErrOutputClosed、
// ほか (ENOSPC など。本家は OSError の内部エラー) はその誤り。EPIPE を受けるには、入口 (main.go) が SIGPIPE を Notify で受けている必要がある。
func WriteTo(w io.Writer, text string) error {
	if _, err := io.WriteString(w, text); err != nil {
		if errors.Is(err, syscall.EPIPE) {
			return ErrOutputClosed
		}
		return err
	}
	return nil
}

// Finish は run の誤り (nil なら rc 0 でなく、呼び出し側が持つ rc) を本家の main の分け方で終了コードにする: 終わり際の書き込みの失敗は 120、
// 本家の BrokenPipeError と、ほかの例外は rc 3 (文面の細部 (トレースバック) は揃えない)。
func Finish(env cmdenv.Env, prog string, err error) int {
	var late LateFlushError
	if errors.As(err, &late) {
		if late.Stream == "stdout" {
			Eprintln(env, "Exception ignored while flushing sys.stdout:\nBrokenPipeError: [Errno 32] Broken pipe")
		}
		return ExitLateFlush
	}
	var internal *visibility.Error
	if errors.As(err, &internal) {
		Eprintln(env, fmt.Sprintf("%s: 内部エラー: %s: %s", prog, internal.Type, internal.Message))
	} else if errors.Is(err, ErrOutputClosed) {
		Eprintln(env, prog+": 出力先が閉じられたため、結果を最後まで書けませんでした。")
	} else {
		Eprintln(env, fmt.Sprintf("%s: 内部エラー: %v", prog, err))
	}
	return ExitInternal
}

// ReadInput は本家の _read_input。path か標準入力 (path が nil か "-") を読み、(文字列, decode_source の情報, "") か (_, _, 誤りの文) を返す。
func ReadInput(env cmdenv.Env, path *string) (string, visibility.SourceInfo, string) {
	if path != nil && *path != "-" {
		data, err := os.ReadFile(*path)
		if err != nil {
			return "", visibility.SourceInfo{}, fmt.Sprintf("Error opening file %s: ファイルを開けません（%s）", *path, visibility.OSErrorReason(*path, err))
		}
		text, info, derr := visibility.DecodeSourceBytes(data)
		if derr != nil {
			return "", visibility.SourceInfo{}, fmt.Sprintf("Error opening file %s: UTF-8 として読めません", *path)
		}
		return text, info, ""
	}
	data, err := io.ReadAll(env.Stdin)
	if err != nil {
		return "", visibility.SourceInfo{}, fmt.Sprintf("Error reading stdin: 標準入力を読めません（%s）", visibility.OSErrorReason("", err))
	}
	text, info, derr := visibility.DecodeSourceBytes(data)
	if derr != nil {
		return "", visibility.SourceInfo{}, "Error reading stdin: UTF-8 として読めません"
	}
	return text, info, ""
}
