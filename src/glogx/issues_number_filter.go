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
//
// 編集キー (カーソルの移動・語の削除・ctrl+u / ctrl+k) は tuikit/lineedit に任せる
// (docs/glogx-ui-guide.md §7。urlPicker も同じ)。足してよい文字が数字だけなのはここで絞る。

import (
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/lineedit"

	"glogx/issues"
)

type issuesNumberFilter struct {
	// active は絞り込みが効いているか。入力を終えた (typing=false) 後も残る。
	active bool
	// typing は検索語を入力中か。true のあいだ一覧のキーは飲まれる。
	typing bool
	line   lineedit.Line // 検索語 (数字だけ)
}

// query は今の検索語。
func (f *issuesNumberFilter) query() string { return f.line.String() }

// start は入力を始める。絞り込み中に呼べば検索語の続きから打てる (打ち直しにしない — 1 文字
// 消したいだけのときに全部消えるのは操作の取り消しとして強すぎる)。
func (f *issuesNumberFilter) start() {
	f.active, f.typing = true, true
}

// confirm は入力を終えて絞り込みを残す。検索語が空なら絞り込みごとやめる (空の絞り込みは
// 「全部見えている」= 絞り込んでいないのと同じで、ヘッダーだけが残ると嘘になる)。
func (f *issuesNumberFilter) confirm() {
	if f.line.Empty() {
		f.clear()
		return
	}
	f.typing = false
}

// clear は絞り込みを捨てる。
func (f *issuesNumberFilter) clear() { *f = issuesNumberFilter{} }

// edit は入力中のキーを検索語へ反映する (検索語が変わったら true)。数字は入れ、数字以外の文字は捨て、
// それ以外は lineedit の編集キーとして渡す (カーソルを動かすだけのキーは false)。
func (f *issuesNumberFilter) edit(key string) bool {
	before := f.line.String()
	switch {
	case isDigitKey(key):
		f.line.Insert(key)
	case isPrintableKey(key):
		return false // 数字以外の文字は検索語にしない (一覧のキーとしても実行しない。冒頭の doc)
	default:
		f.line.Key(key, "") // text を渡さないので、編集キー以外は何も入れない
	}
	return f.line.String() != before
}

// paste は貼り付けた文字列のうち、最初に出てくる数字の並びだけを検索語に入れる (検索語が変わったら true)。
// 「#415」「issue 415」や、glogx の Y でコピーした参照 (「issue 415 タイトル (issues/415-x.md)」) を貼っても 415 で引ける。
// 数字を全部つなげると参照の番号とパスの番号が重なって 415415 になり、何にも一致しない (敵対レビューが実測)。
//
// 先に termsafe で無害化する (エスケープの中の数字を拾わない: "\x1b[31m415" を 31 と読む。敵対レビューが実測)。
// 最初の数字の並びが番号でない貼り付け (先頭に日付がある等) は外れるが、Y の参照は「issue N …」で始まるので起きない。
func (f *issuesNumberFilter) paste(s string) bool {
	s = sanitizePlainLine(s)
	start := strings.IndexFunc(s, isASCIIDigit)
	if start < 0 {
		return false
	}
	run := s[start:]
	if end := strings.IndexFunc(run, func(r rune) bool { return !isASCIIDigit(r) }); end >= 0 {
		run = run[:end]
	}
	f.line.Insert(run)
	return true
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

// rows は番号に検索語を含む issue を、渡された並びのまま返す。検索語が空なら全件
// (入力を始めた直後に一覧が消えると、何を絞り込んでいるのか分からなくなる)。
func (f *issuesNumberFilter) rows(all []*issues.Issue) []*issues.Issue {
	q := f.query()
	out := make([]*issues.Issue, 0, len(all))
	for _, iss := range all {
		if q == "" || strings.Contains(iss.Number, q) {
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
