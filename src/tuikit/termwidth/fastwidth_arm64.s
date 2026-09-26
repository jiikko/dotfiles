// fastDispWidthAsm の arm64 実装。受理規則の正本は termwidth.go の fastDispWidthGeneric で、
// ここはその写し。分岐を足すときは Go 版と同時に直し、FuzzFastDispWidthMatchesGeneric を回す。
//
// 構造: 印字可能 ASCII (0x20..0x7e) の連なりを 16 byte ずつ NEON で判定して一括で足し、
// 連なりが切れた byte (ESC / UTF-8 の先頭 / それ以外) だけをスカラで処理する。
// glogx の行は大半が ASCII の連なりなので、Go 版の「1 byte ごとに switch」を 16 byte に 1 回へ減らす。
//
// レジスタ:
//   R0 = 現在位置 (ポインタ) / R1 = 末尾 (ポインタ) / R2 = 幅の累計 / R9 = symWidthTable
//   V30 = 0x20 × 16 / V31 = 0x7e × 16

#include "go_asm.h"
#include "textflag.h"

// func fastDispWidthAsm(s string) (int, bool)
TEXT ·fastDispWidthAsm(SB), NOSPLIT, $0-25
	MOVD	s_base+0(FP), R0
	MOVD	s_len+8(FP), R1
	ADD	R0, R1, R1
	MOVD	ZR, R2
	MOVD	$·symWidthTable(SB), R9
	MOVD	$0x20, R3
	VDUP	R3, V30.B16
	MOVD	$0x7e, R3
	VDUP	R3, V31.B16

loop:
	SUB	R0, R1, R3
	CMP	$16, R3
	BLT	scalar

	// 16 byte が全部 0x20..0x7e か: clamp(v, 0x20, 0x7e) == v の lane が all-ones になる
	VLD1	(R0), [V0.B16]
	VUMAX	V30.B16, V0.B16, V1.B16
	VUMIN	V31.B16, V1.B16, V1.B16
	VCMEQ	V0.B16, V1.B16, V1.B16
	VMOV	V1.D[0], R4
	VMOV	V1.D[1], R5
	AND	R4, R5, R6
	CMN	$1, R6
	BNE	partial
	ADD	$16, R0
	ADD	$16, R2
	B	loop

partial:
	// 最初の非印字 byte の位置 = 反転した mask の最下位の立っている byte (little endian)
	CMN	$1, R4
	BNE	partial_lo
	MVN	R5, R5
	RBIT	R5, R5
	CLZ	R5, R5
	LSR	$3, R5, R5
	ADD	$8, R5, R5
	B	partial_adv

partial_lo:
	MVN	R4, R4
	RBIT	R4, R4
	CLZ	R4, R5
	LSR	$3, R5, R5

partial_adv:
	ADD	R5, R0
	ADD	R5, R2
	B	special // R0 は非印字 byte を指している (R0 < R1 は上の判定が保証する)

scalar:
	// 16 byte 未満の残り: 1 byte ずつ
	CMP	R1, R0
	BHS	done
	MOVBU	(R0), R4
	SUB	$0x20, R4, R5
	CMP	$0x5f, R5 // 符号なしで 0x20..0x7e ⇔ (c - 0x20) < 0x5f
	BHS	special
	ADD	$1, R0
	ADD	$1, R2
	B	scalar

special:
	MOVBU	(R0), R4
	CMP	$0x1b, R4
	BEQ	esc
	// C0 制御・DEL・継続 byte 単独・overlong の 2 byte 先頭 (0xc0/0xc1) は受理しない。
	// 0x80 未満で印字可能でないものも、0xc2 未満の判定で一緒に落ちる
	CMP	$0xc2, R4
	BLO	fail
	CMP	$0xe0, R4
	BLO	utf8_2
	// 3 byte 列で表 (上限 symTableHi = 0x28ff) に届く先頭は 0xe0..0xe2 だけ。
	// 0xe3 以上と 4 byte 列は表の外なので受理しない
	CMP	$0xe2, R4
	BHI	fail
	ADD	$3, R0, R6
	CMP	R1, R6
	BHI	fail
	MOVBU	1(R0), R5
	MOVBU	2(R0), R7
	AND	$0xc0, R5, R8
	CMP	$0x80, R8
	BNE	fail
	AND	$0xc0, R7, R8
	CMP	$0x80, R8
	BNE	fail
	AND	$0x0f, R4, R4
	AND	$0x3f, R5, R5
	AND	$0x3f, R7, R7
	LSL	$12, R4, R4
	ORR	R5<<6, R4, R4
	ORR	R7, R4, R4
	CMP	$0x800, R4 // overlong (0xe0 0x80..0x9f ..) は utf8.DecodeRuneInString が RuneError にする
	BLO	fail
	B	lookup

utf8_2:
	ADD	$2, R0, R6
	CMP	R1, R6
	BHI	fail
	MOVBU	1(R0), R5
	AND	$0xc0, R5, R8
	CMP	$0x80, R8
	BNE	fail
	AND	$0x1f, R4, R4
	AND	$0x3f, R5, R5
	LSL	$6, R4, R4
	ORR	R5, R4, R4

lookup:
	// R4 = rune / R6 = 次の位置。表の範囲外 (下も上も) は符号なし比較 1 回で落とす
	SUB	$const_symTableLo, R4, R4
	CMP	$(const_symTableHi-const_symTableLo), R4
	BHI	fail
	MOVBU	(R9)(R4), R5
	CBZ	R5, fail
	SUB	$1, R5, R5 // 表は幅 +1 を持つ (0 = 受理しない)
	ADD	R5, R2
	MOVD	R6, R0
	B	resume

esc:
	// SGR (ESC [ 数字と ; の並び m) だけを幅 0 として飛ばす
	ADD	$1, R0, R6
	CMP	R1, R6
	BHS	fail
	MOVBU	(R6), R5
	CMP	$0x5b, R5 // '['
	BNE	fail
	ADD	$1, R6

esc_params:
	CMP	R1, R6
	BHS	fail // 途中で切れた列
	MOVBU	(R6), R5
	CMP	$0x3b, R5 // ';'
	BEQ	esc_next
	SUB	$0x30, R5, R7 // '0'..'9'
	CMP	$10, R7
	BLO	esc_next
	CMP	$0x6d, R5 // 'm'
	BNE	fail      // SGR 以外の CSI
	ADD	$1, R6, R0
	B	resume

esc_next:
	ADD	$1, R6
	B	esc_params

resume:
	// 切れ目の直後は、次の 1 byte をスカラで見てから NEON に戻る。罫線のように記号が連続する行で
	// 記号 1 字ごとに 16 byte の判定と位置計算を払わないため (払うと Go 版の 1.8 倍遅かった。
	// macos-15 runner の BenchmarkFastDispWidth/box_120 実測 2026-09-26)
	CMP	R1, R0
	BHS	done
	MOVBU	(R0), R4
	SUB	$0x20, R4, R5
	CMP	$0x5f, R5
	BHS	special
	B	loop

done:
	MOVD	R2, ret+16(FP)
	MOVD	$1, R3
	MOVB	R3, ret1+24(FP)
	RET

fail:
	MOVD	ZR, ret+16(FP)
	MOVB	ZR, ret1+24(FP)
	RET
