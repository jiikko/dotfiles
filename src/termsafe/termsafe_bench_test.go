package termsafe

import (
	"strings"
	"testing"
)

// 関門を通る文字列の典型 (epic 523: asm の候補かを判断する単体の計測)。大半は無害化の要らない文字列で、
// fast path (VS16 の除去 → UTF-8 の検査 → 制御文字の探索) だけを通る。
var benchInputs = []struct{ name, s string }{
	{"ascii_line_120B", strings.Repeat("go test ./... -count=1 ok ", 5)[:120]},
	{"ascii_block_8KB", strings.Repeat("2026-09-27T07:55:00.123Z ok  \tglogx\t0.412s coverage: 81.2% of statements\n", 110)},
	{"ascii_long_8KB", strings.Repeat("go test ./... -count=1 ok glogx 0.412s coverage 81.2% ", 150)},
	{"ja_line", strings.Repeat("カードの詳細を開いて差分を確かめる。", 4)},
	{"ja_long_8KB", strings.Repeat("見ているだけの画面なので、カードの作成は別のターミナルで行う。", 90)},
	{"ja_block_8KB", strings.Repeat("見ているだけの画面なので、カードの作成は別のターミナルで行う。\n", 90)},
	{"ci_log_sgr_line", "\x1b[36;1mRun go test ./...\x1b[0m \x1b[32mok\x1b[0m  glogx  0.412s"},
	{"ci_log_sgr_block_8KB", strings.Repeat("\x1b[36;1m2026-09-27T07:55:00Z\x1b[0m \x1b[32mok\x1b[0m  glogx\t0.412s coverage 81.2%\n", 110)},
}

func BenchmarkPlainLine(b *testing.B) {
	for _, in := range benchInputs {
		b.Run(in.name, func(b *testing.B) {
			b.SetBytes(int64(len(in.s)))
			b.ReportAllocs()
			for b.Loop() {
				_ = PlainLine(in.s)
			}
		})
	}
}

func BenchmarkPlainBlock(b *testing.B) {
	for _, in := range benchInputs {
		b.Run(in.name, func(b *testing.B) {
			b.SetBytes(int64(len(in.s)))
			b.ReportAllocs()
			for b.Loop() {
				_ = PlainBlock(in.s)
			}
		})
	}
}

// fast path の内訳 (どの走査が重いか)。asm にするならこの 3 つのどれか (または 1 本にまとめたもの) が対象。
// 🚨 改行・タブを含む入力 (*_block_*) は needsSanitize が真で fast path を通らない (PlainBlock でも同じ) ので、ここには入れない
func BenchmarkFastPathParts(b *testing.B) {
	for _, in := range benchInputs {
		if needsSanitize(in.s) {
			continue
		}
		b.Run("DropEmojiVS16/"+in.name, func(b *testing.B) {
			b.SetBytes(int64(len(in.s)))
			for b.Loop() {
				_ = DropEmojiVS16(in.s)
			}
		})
		b.Run("needsSanitize/"+in.name, func(b *testing.B) {
			b.SetBytes(int64(len(in.s)))
			for b.Loop() {
				_ = needsSanitize(in.s)
			}
		})
	}
}
