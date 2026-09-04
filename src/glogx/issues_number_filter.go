package main

// issuesNumberFilter は一覧を issue 番号で絞り込むインクリメンタルフィルタ (一覧で /)。
//
// 対象はタブ (カテゴリ) と状態フィルタの両方を無視した全 issue にする。番号で引くのは「その
// issue へ飛びたい」ときで、415 を探しているのに done だから出てこないのでは用を成さない。
// 代わりに絞り込み中はヘッダーに「全カテゴリ・全状態」と出す (numberFilterLine)。書かないと
// タブ行のバッジと画面に並ぶ行が食い違って見える。
//
// 入力の作法は urlPicker (fzf 流) に揃える: 打った文字が検索語、移動は ctrl+n/p と矢印、Enter
// で確定、Esc で解除。🚨 数字以外の印字文字は「無視」であって「一覧のキーとして実行」ではない。
// 番号検索に限れば j/k は検索語にならないので移動へ回せるが、本文・タイトル検索を足した日に
// j/k の意味が変わってしまう。今から urlPicker と同じ作法に寄せておく。
//
// Enter を「確定して絞り込みは残す」にしているのは、y / p / n を絞り込み結果へ効かせるため。
// ピッカーのように選んで閉じる形にすると、フィルタとしては使えない。

import (
	"strings"

	"glogx/issues"
)

type issuesNumberFilter struct {
	// active は絞り込みが効いているか。入力を終えた (typing=false) 後も残る。
	active bool
	// typing は検索語を入力中か。true のあいだ一覧のキーは飲まれる。
	typing bool
	query  string
}

// start は入力を始める。絞り込み中に呼べば検索語の続きから打てる (打ち直しにしない — 1 文字
// 消したいだけのときに全部消えるのは操作の取り消しとして強すぎる)。
func (f *issuesNumberFilter) start() {
	f.active, f.typing = true, true
}

// confirm は入力を終えて絞り込みを残す。検索語が空なら絞り込みごとやめる (空の絞り込みは
// 「全部見えている」= 絞り込んでいないのと同じで、ヘッダーだけが残ると嘘になる)。
func (f *issuesNumberFilter) confirm() {
	if f.query == "" {
		f.clear()
		return
	}
	f.typing = false
}

// clear は絞り込みを捨てる。
func (f *issuesNumberFilter) clear() { *f = issuesNumberFilter{} }

// edit は入力中のキーを検索語へ反映する (検索語が変わったら true)。数字と編集キー以外は捨てる。
func (f *issuesNumberFilter) edit(key string) bool {
	switch key {
	case "backspace", "ctrl+h":
		// ctrl+h は backspace の別名 (端末によっては 0x08 で届く。urlPicker と同じ扱い)
		if f.query == "" {
			return false
		}
		r := []rune(f.query)
		f.query = string(r[:len(r)-1])
		return true
	case "ctrl+u":
		if f.query == "" {
			return false
		}
		f.query = ""
		return true
	}
	if !isDigitKey(key) {
		return false
	}
	f.query += key
	return true
}

// rows は番号に検索語を含む issue を、渡された並びのまま返す。検索語が空なら全件
// (入力を始めた直後に一覧が消えると、何を絞り込んでいるのか分からなくなる)。
func (f *issuesNumberFilter) rows(all []*issues.Issue) []*issues.Issue {
	out := make([]*issues.Issue, 0, len(all))
	for _, iss := range all {
		if f.query == "" || strings.Contains(iss.Number, f.query) {
			out = append(out, iss)
		}
	}
	return out
}

// groupKeys は番号フィルタの結果に含まれる Epic の GroupKey を返す。番号フィルタ中だけ親を
// 自動展開し、解除時には呼び出し側がこの集合を捨てる (手動の展開状態とは分離する)。
func (f *issuesNumberFilter) groupKeys(rows []*issues.Issue) map[string]bool {
	if !f.active {
		return nil
	}
	out := make(map[string]bool)
	for _, iss := range rows {
		if iss.GroupKind != issues.GroupEpic {
			continue
		}
		if iss.GroupKey != "" {
			out[iss.GroupKey] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isDigitKey は検索語に足してよい 1 文字か (数字のみ)。修飾キー付き ("ctrl+x") や名前付きキー
// ("pgdown") は 1 ルーンでないので自然に弾かれる。
func isDigitKey(key string) bool {
	r := []rune(key)
	return len(r) == 1 && r[0] >= '0' && r[0] <= '9'
}
