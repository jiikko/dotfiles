package app

import "strings"

// NormalizeSpaceKey は Space の表記ゆれを " " へ揃える。キー switch は " " だけを見ればよい。
//
// なぜ 2 表記あるか: bubbletea v2 の KeyPressMsg.String() は Space を "space" と綴る (v1 は
// " ")。一方、複数ルーンが 1 イベントで届いたときの分解経路 (Update の KeyPressMsg 処理) は
// 生のルーン文字列を渡すので " " が来る。v2 移行時にこの綴りの変化を拾い落として、Space の
// 割当 5 箇所 (issues viewer の半ページ / diff pager / job 詳細を閉じる / パネルを開く) が
// 全て無反応になっていた (ユーザー報告 2026-07-31)。入口で正規化して 1 箇所に閉じる。
func NormalizeSpaceKey(key string) string {
	if key == "space" {
		return " "
	}
	return key
}

// NoticeKind は viewer の操作結果の種類。showWarning の 3 分類 (showWarning の doc) を viewer からも
// 表せるようにする: ok bool の 2 値にしない。断りの文やクリップボード失敗まで lastWarning を
// 上書きし、直前のエラーを w でコピーできなくなる。
type NoticeKind uint8

const (
	NoticeOK      NoticeKind = iota // 成功 (トーストのみ)
	NoticeRefused                   // 操作を拒否した理由の案内・クリップボード失敗 (失敗トーストのみ。lastWarning を汚さない)
	NoticeError                     // エラー詳細を含み、後で w でコピーする価値があるもの (showWarning)
)

// ClipboardFailText はクリップボードへのコピーが失敗したときの通知文 (copyWithToast と viewer で共有)。
// 通知の種類は NoticeRefused (lastWarning を汚さない。showWarning の doc の 3 分類)。
func ClipboardFailText(err error) string {
	return "コピーに失敗しました: " + FirstLine(err.Error())
}

func ClampIdx(i, total int) int {
	if total <= 0 {
		return 0
	}
	return min(max(i, 0), total-1)
}

func FirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
