// Package ctlprobe は「端末が制御として解釈しうる文字が残っているか」を判定する、
// **テスト専用**のオラクル。
//
// 🚨 termsafe の production コードから導出してはいけない (issue 285)。無害化テストの
// 期待値を無害化実装から作ると自己言及になり、実装と一緒に間違える
// (`adversarial-review-own-safeguards.md` の 0-B)。ここは**独立した言い換え**として、
// 「C0 / DEL / C1 のどれかが残っていたら真」を素朴に書く。
//
// 🚨 ESC と BEL だけを見る判定にしないこと: それだと 8bit の CSI (U+009B) / OSC (U+009D) を
// 原理的に見逃し、「ESC と BEL だけ落とす」実装がテストを全部 green で通ってしまう
// (敵対的レビュー 2026-08-05 が実際にこの盲点を突いた)。
//
// 🚨 この「導出してはいけない」は**構造で強制されている**（コメントだけの約束ではない）:
// termsafe の内部テスト（`package termsafe` の termsafe_test.go）がこのパッケージを import
// しているので、ここから termsafe を import すると `import cycle not allowed in test` で
// go test が落ちる（実測 2026-09-06）。つまり自己言及の変異は CI が赤で止める。
// ただし強制の足場は**その 1 ファイルの package 節**なので、termsafe_test.go を外部テスト
// パッケージ（`package termsafe_test`）へ変えると循環が合法になり、この防御は消える。
// 変えるなら、代わりの検出手段を同じ commit で用意すること。
//
// なぜ 1 箇所に置くか: 同じ判定が glogx / glogx/issues / termsafe / doctor/disk / doctor/svc の
// 5 箇所にバイト一致で複製されていた (issue 285)。無害化の定義を広げる (U+2028/2029、
// CSI の別形など) とき、**4 箇所を直し忘れても全パッケージ green のまま**その関門だけ
// 旧い狭いオラクルで守り続ける。touch 箇所を 5 → 1 にする。
package ctlprobe

// HasControl は s に C0 (< 0x20) / DEL (0x7f) / C1 (0x80..0x9f) が含まれるか。
func HasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
	}
	return false
}

// HasControlExceptNewline は改行を許す版 (コピー用の文字列は複数行が正常)。
func HasControlExceptNewline(s string) bool {
	for _, r := range s {
		if r == '\n' {
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
	}
	return false
}
