package main

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"doctor/runner"
	"doctor/ssd"
)

// doctor の SSD タブ (issue 578)。内蔵 SSD の健康状態を**読むだけ**で、修復・sudo・APFS の検査は
// 実行しない (APFS は手で叩くコマンドを y でコピーさせるだけ)。判定は doctor/ssd が正本。
//
// 🚨 **結果はキャッシュに載せない** (Docker と同じ)。diskutil 0.07 秒 / smartctl 0.02 秒 (実測
// 2026-09-29) なので開くたびに取り直せばよく、保存すると「古い値が今の値に見える」形と
// 復元経路の無害化を新設することになる。snapshot を復元する経路でも走らせる (uncachedCmds)。

type doctorSSDMsg struct {
	gen int
	rep ssd.Report
}

const (
	ssdAPFSRowKey     rowKey = "ssd:apfs"
	ssdSmartctlRowKey rowKey = "ssd:smartctl"
)

// ssdInstallToast は smartctl が無いときに doctor を開いた直後に出す案内。
const ssdInstallToast = "SSD の摩耗・整合性エラーは smartctl で読みます: " + ssd.InstallHint

func (v *doctorView) ssdCmd(ctx context.Context, gen int) tea.Cmd {
	opt := ssd.Options{Run: runner.Exec}
	if v.ssdOpts != nil {
		opt = v.ssdOpts()
	}
	return func() tea.Msg {
		var rep ssd.Report
		doctorTrack(func() { rep = ssd.Scan(ctx, opt) })
		return doctorSSDMsg{gen: gen, rep: rep}
	}
}

// receiveSSD は結果を受け取り、出すべき toast の文言を返す ("" = 出さない)。
//
// toast は smartctl が無いときだけ。**開くたびに出す** (ユーザー指定 2026-09-29) — 入れるまで
// SSD の判定が「判定不能」のままなので、黙って放置させない。
func (v *doctorView) receiveSSD(msg doctorSSDMsg) string {
	if msg.gen != v.gen || !v.shown {
		return ""
	}
	rep := msg.rep
	v.ssd = &rep
	if rep.SmartctlMissing {
		return ssdInstallToast
	}
	return ""
}

// ssdMark は状態の印と語。**画面の語彙は disk / docker と揃える** (✅ / 🚨 / ⛔)。
// 🚨 exhaustive (default なし) なので、ssd.Status が増えたらここが compile error になる。
func ssdMark(s ssd.Status) string {
	switch s {
	case ssd.StatusOK:
		return "✅ ok"
	case ssd.StatusWarn:
		return "🚨 注意"
	case ssd.StatusFail:
		return "⛔ 異常"
	case ssd.StatusUnknown:
		return "❓ 判定不能"
	case ssd.StatusUnchecked:
		return "・ 未検査"
	case ssd.StatusInfo:
		return ""
	}
	return string(s)
}

// ssdWord は見出しの要約に使う語 (印なし)。
func ssdWord(s ssd.Status) string {
	mark := ssdMark(s)
	if _, word, ok := strings.Cut(mark, " "); ok {
		return word
	}
	return mark
}

// ssdTabSummary はタブ行の印。**SSD の行の印 (ssdMark) の先頭と同じにする** — タブ行の 🚨 を
// 追って開いた先に 🚨 の行が無い、を作らない (敵対レビュー 2 周目)。どれも 2 桁なので、狭い幅で
// タブ行の末尾が切られても要約が「注…」のように欠けない。
// 🚨 そのため判定不能は ❓ で、ほかのタブの「診断できず」(🚨) とは印が違う (SSD の 🚨 は注意)
func ssdTabSummary(s ssd.Status) string {
	mark, _, _ := strings.Cut(ssdMark(s), " ")
	if mark == "" {
		return "❓"
	}
	return mark
}

func (v *doctorView) ssdSection(o doctorRenderOpts) []doctorRow {
	if v.ssd == nil {
		return sectionHeader(o, "SSD", o.spinner+" diskutil / smartctl を実行中")
	}
	rep := *v.ssd
	// 🚨 SMART と APFS を**必ず並べて**出す。SMART が ok でも「ディスクが正常」とは総括しない。
	// sectionHeader は幅が足りないと要約を落とすので、そのときは型番の方を削る
	summary := "SMART: " + ssdWord(rep.SMART) + " / APFS: " + ssdWord(rep.APFS)
	title := "SSD"
	if rep.Model != "" {
		full := title + " (" + rep.Model
		if rep.Internal {
			full += " / 内蔵"
		}
		full += ")"
		if sectionHeaderGap(o, full, summary) >= 1 {
			title = full
		}
	}
	rows := sectionHeader(o, title, summary)

	labelW, markW := 0, 0
	for _, c := range rep.Checks {
		labelW = max(labelW, dispWidth(c.Label))
		markW = max(markW, dispWidth(ssdMark(c.Status)))
	}
	labelW = max(labelW, dispWidth("APFS (Data)"))
	markW = max(markW, dispWidth(ssdMark(rep.APFS)))
	cell := func(label string, st ssd.Status) string {
		mark := ssdMark(st)
		return label + padSpaces(labelW-dispWidth(label)) + "  " +
			doctorColor(o.colored, ssdColor(st), mark) + padSpaces(markW-dispWidth(mark)) + "  "
	}
	for _, c := range rep.Checks {
		text := cell(c.Label, c.Status) + c.Value
		if c.Note != "" && c.Status != ssd.StatusOK && c.Status != ssd.StatusInfo {
			text += doctorColor(o.colored, ansiDim, "  "+c.Note)
		}
		row := doctorRow{text: text}
		if c.Label == "smartctl" && rep.SmartctlMissing {
			// 入れ方をコピーできる行にする (doctor からは入れない)
			row.selectable, row.key = true, ssdSmartctlRowKey
			row.copyPath = ssd.InstallHint
			row.copyText = "SSD の摩耗・整合性エラーを読む smartctl が入っていません。次で入れられます:\n" + ssd.InstallHint
		}
		rows = append(rows, row)
	}
	rows = append(rows, doctorRow{
		text: cell("APFS (Data)", rep.APFS) +
			doctorColor(o.colored, ansiDim, "y で検査コマンドをコピー (doctor は実行しません。数分かかります)"),
		selectable: true,
		key:        ssdAPFSRowKey,
		copyPath:   ssd.APFSCommand,
		copyText:   ssdCopyText(rep),
	})
	for _, n := range rep.Notes {
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "  "+n)})
	}
	return rows
}

func ssdColor(s ssd.Status) string {
	switch s {
	case ssd.StatusOK:
		return ansiGreen
	case ssd.StatusWarn, ssd.StatusUnknown:
		return ansiYellow
	case ssd.StatusFail:
		return ansiRed
	case ssd.StatusUnchecked, ssd.StatusInfo:
		return ansiDim
	}
	return ansiDim
}

// ssdCopyText は Y の解説 (別セッションの LLM にそのまま渡せる形)。値はすべて関門を通った後のもの。
func ssdCopyText(rep ssd.Report) string {
	var b strings.Builder
	b.WriteString("内蔵 SSD の健康状態 (glogx doctor)")
	if rep.Model != "" {
		b.WriteString(": " + rep.Model)
	}
	b.WriteString("\nSMART: " + ssdWord(rep.SMART) + " / APFS: " + ssdWord(rep.APFS) + "\n")
	for _, c := range rep.Checks {
		b.WriteString("- " + c.Label + ": " + c.Value)
		if w := ssdWord(c.Status); w != "" {
			b.WriteString(" [" + w + "]")
		}
		if c.Note != "" {
			b.WriteString(" (" + c.Note + ")")
		}
		b.WriteString("\n")
	}
	for _, n := range rep.Notes {
		b.WriteString("- " + n + "\n")
	}
	b.WriteString("APFS の整合性は未検査。手で確かめるなら: " + ssd.APFSCommand + "\n")
	return b.String()
}
