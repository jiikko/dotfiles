package ui

// ? で出すヘルプ。「流れ / レーン / 印とポイント / 役」のタブに分ける (tab で切り替え。設定画面 456 と同じキー。issue 515)。
// 説明の正本は card の側 (流れ = card.MainFlow / card.FlowDetours / card.AfterDone、レーン = card.State.Meaning、
// ポイント = card.PointsMeaning) と、この下の roleMeanings / HumansTurnMeaning (ここは並べて見せるだけ)。

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/layout"
	"tuikit/sgr"

	"pro-con/card"
)

// legendTab は ? のタブ。
type legendTab int

const (
	legendFlow  legendTab = iota // カードの流れ (誰が・何で)
	legendLanes                  // レーンごとの意味
	legendMarks                  // 人の番の印と見積もりのポイント
	legendRoles                  // 役 (プロセス) の仕事
)

// legendTabs はタブの並び (tab で右へ、端で回る)。個数の番兵を enum に混ぜない (switch の網羅の検査が番兵の case を求める)。
var legendTabs = []legendTab{legendFlow, legendLanes, legendMarks, legendRoles}

func (t legendTab) label() string {
	switch t {
	case legendFlow:
		return "流れ"
	case legendLanes:
		return "レーン"
	case legendMarks:
		return "印とポイント"
	case legendRoles:
		return "役"
	}
	return ""
}

const legendTitle = " カードの流れと意味 "

// handleLegendKey は表を出している間のキー。? / q / esc で閉じ、tab / shift+tab でタブを替え、ctrl+c は終了の手続きへ。それ以外は飲み込む
// (表の下でカンバンが動くと、閉じたときに居場所が変わって見える)。
func (m *Model) handleLegendKey(key string) tea.Cmd {
	switch key {
	case "?", "q", "esc":
		m.legend, m.legendOff = false, 0
	case "tab", "shift+tab":
		n := len(legendTabs)
		step := 1
		if key == "shift+tab" {
			step = n - 1
		}
		m.legendTab, m.legendOff = legendTabs[(slices.Index(legendTabs, m.legendTab)+step)%n], 0
	case "j", "down":
		m.legendOff++ // 下限・上限は描くとき (overlayLegend) に幅と高さから決める
	case "k", "up":
		m.legendOff = max(m.legendOff-1, 0)
	case "g":
		m.legendOff = 0
	case "G":
		m.legendOff = 1 << 20
	case "ctrl+c":
		m.legend = false
		return m.requestQuit()
	}
	return nil
}

// overlayLegend は表を画面の中央に重ねる。タブの行は送らずに頭に残す。
func (m *Model) overlayLegend(screen []string) []string {
	if !m.legend {
		return screen
	}
	width, inner := legendSize(m.width)
	head := []string{legendTabBar(m.legendTab, inner), ""}
	body := legendRows(m.legendTab, inner)
	title := legendTitle
	// 画面より長いときは j / k で送る (板は上下の辺と影で 3 行使う。上下に 1 行ずつ空ける)
	if avail := max(len(screen)-5-len(head), 3); len(body) > avail {
		m.legendOff = min(m.legendOff, len(body)-avail)
		title = fmt.Sprintf("%s(j / k で送る %d/%d) ", legendTitle, m.legendOff+avail, len(body))
		body = body[m.legendOff : m.legendOff+avail]
	} else {
		m.legendOff = 0
	}
	box := layout.Panel(title, append(head, body...), width, true, layout.PanelStyle{Border: layout.BorderLight, Color: sgr.Dim})
	return layout.OverlayCentered(screen, box, m.width, len(screen), true)
}

// legendTabBar はタブの行 (今のタブを反転)。幅 inner に収まらなければ、今のタブの名前と位置だけにする。
func legendTabBar(cur legendTab, inner int) string {
	var tabs []string
	for _, t := range legendTabs {
		if t == cur {
			tabs = append(tabs, settingsReverse+sgrBold+" "+t.label()+" "+sgrReset)
		} else {
			tabs = append(tabs, sgrDim+" "+t.label()+" "+sgrReset)
		}
	}
	bar := strings.Join(tabs, "  ")
	if hint := sgrDim + "   tab で切り替え" + sgrReset; ansi.StringWidth(bar+hint) <= inner {
		return bar + hint
	} else if ansi.StringWidth(bar) <= inner {
		return bar
	}
	return fmt.Sprintf("%s %s %d/%d  tab で次へ%s", settingsReverse+sgrBold, cur.label(), slices.Index(legendTabs, cur)+1, len(legendTabs), sgrReset)
}

// legendSize は画面の幅 total に対する表の板の幅と、中身の幅。
// 🚨 Panel の width は右の影 1 桁込みなので、中身の幅は width-1 の枠から数える (1 桁広く折り返すと行末が … で切れる)
func legendSize(total int) (width, inner int) {
	width = min(total-4, 76)
	return width, layout.PanelInnerWidth(width - 1)
}

// legendRows はタブ tab の中身の行 (どの行も幅 inner に収まるよう折り返す)。
func legendRows(tab legendTab, inner int) []string {
	var rows []string
	// explain は説明の文を幅に収まるよう折り返し、見出しの下に indent 桁下げて足す
	explainAt := func(indent int, text string) {
		pad := strings.Repeat(" ", indent)
		for _, l := range strings.Split(ansi.Hardwrap(text, max(inner-indent, 10), true), "\n") {
			rows = append(rows, pad+l)
		}
	}
	explain := func(text string) { explainAt(2, text) }
	switch tab {
	case legendFlow:
		for i, sec := range []struct {
			title string
			steps []card.FlowStep
		}{{"通常の流れ", card.MainFlow}, {"寄り道", card.FlowDetours}} {
			if i > 0 {
				rows = append(rows, "")
			}
			rows = append(rows, sgrBold+sec.title+sgrReset)
			for _, st := range sec.steps {
				rows = append(rows, flowHeading(st))
				explainAt(4, st.Who+": "+st.How)
			}
		}
		rows = append(rows, "", sgrBold+"完了の後"+sgrReset)
		for _, a := range card.AfterDone {
			explain(a)
		}
		rows = append(rows, "", sgrBold+"attach から pro-con へ戻る"+sgrReset) // issue 527
		for _, a := range attachReturnHelp {
			explain(a)
		}
	case legendLanes:
		for i, s := range card.Columns {
			rows = append(rows, fg(stateColor(s))+sgrBold+fmt.Sprintf("%d %s", i+1, s.Label())+sgrReset)
			explain(s.Meaning())
		}
	case legendMarks:
		rows = append(rows, humanTag(humanMark+"の番")) // 印の意味 (452)。どれが人の番かの正本は card.Turn
		explain(HumansTurnMeaning)
		rows = append(rows, "", sgrBold+"右上の 3pt = 見積もりのポイント"+sgrReset) // カードの右上の数 (490)。意味の正本は card.PointsMeaning
		for _, m := range card.PointsMeaning {
			explain(m)
		}
	case legendRoles: // 役の説明の正本はここ (roleMeanings)。pro-con ps / 設定画面のプロセスの名前と揃える
		for _, r := range roleMeanings {
			rows = append(rows, sgrBold+r[0]+sgrReset)
			explain(r[1])
		}
	}
	return rows
}

// flowHeading は移り変わりの見出し (レーンはレーンの色。列を変えないものは「のまま」)。
func flowHeading(st card.FlowStep) string {
	end := func(e card.FlowEnd) string {
		if e.Other != "" {
			return sgrBold + e.Other + sgrReset
		}
		return fg(stateColor(e.Lane)) + sgrBold + fmt.Sprintf("%d %s", slices.Index(card.Columns, e.Lane)+1, e.Lane.Label()) + sgrReset
	}
	if st.Stays() {
		return end(st.From) + sgrDim + "のまま" + sgrReset
	}
	return end(st.From) + sgrDim + " → " + sgrReset + end(st.To)
}

// RoleMeanings は役と仕事の組の写し (pro-con help terms が並べる。正本は roleMeanings)。
func RoleMeanings() [][2]string { return slices.Clone(roleMeanings) }

// roleMeanings は pro-con のプロセスの役と、その仕事 (? の表に出す。役の名前は pro-con ps の Role と揃える)。
var roleMeanings = [][2]string{
	{"dispatcher", "書き手はこれだけ。受付の箱の依頼を記録に適用し、PG・PM・取り込みの係を起こす・再開する・止める。利用枠を見て PG の数を絞り、落ちた PG の見張り (watchdog) と、起動時の復旧もする。新版のビルドができたら区切りで自分を入れ替える (PG は止めない)"},
	{"supervisor", "画面が起こすワンショットの見張り役。dispatcher を子として持ち、落ちたら間を空けて起こし直し、落ち続けたら PG を止めて諦める。dispatcher が抜けたら一緒に抜ける (常駐しない)"},
	{"PM", "依頼の列のカードを読み、issue に分けてキューに積む (順番と見積もりのポイントも付ける)。PG の質問に答え、人の判断が要るものは人に回す。レビュー・取り込みはしない"},
	{"PG", "カード 1 枚につき 1 つ。自分の worktree とブランチで実装し、時間のかかるコマンドはテストの係に頼み、終えたらレビューに出す。master へは push しない"},
	{"取り込みの係", "レビューの列のカードの diff とテストを確かめ、よければ master へ取り込んで閉じる。衝突やテストの失敗は PG へ差し戻し、自分で片付けられないものは人に回す"},
	{"見張り", "dispatcher とは別のプロセスで、読むだけ。PG のブランチどうし・master との取り込みの衝突と、テストの順番待ちの長さを見て、見つけたことを知らせる"},
	{"テストの係", "dispatcher の中の係。PG が頼んだ make test などを PG の worktree で 1 本ずつ順に走らせ、結果を渡して PG を再開する (失敗は要約の係が要約する)"},
	{"要約の係", "dispatcher がその場で起こす短い haiku。テストの係の失敗の要約と btw の答えを書く。印を持たないので pro-con ps には出ない"},
	{"画面", "カードを映し、人の依頼・回答・差し戻しを受付の箱に置く。持ち主の画面の最後の 1 つを閉じると dispatcher と PG を止める (--join は止めない・--view は読むだけ)"},
}

// HumansTurnMeaning は人の番の意味 (? の表と pro-con help terms が出す)。
const HumansTurnMeaning = "人が操作しないと進まない (権限の確認・落ち続けて止めた PG・PM か取り込みの係が人に回したもの・" +
	"起こさない設定の役の仕事)。黄の字はこれだけに使う"
