package lanmgr

// 操作のログ (ユーザー要望 2026-10-16「俺の操作ログを目で確認できるようになって欲しい。何が詰まっているのか見えて欲しい」)。
// 押したキーとそのときの画面・知らせ・相手を読んだ結果・起こしたコマンドと戻りを 1 行ずつ Options.Log へ渡す (live がファイルへ書く)。
// 入力欄の打鍵は伏せる (別名・ユーザー名・絞り込み・帯。打鍵のまま残すと読みにくい)。確定した別名・ユーザー名は知らせとして残る。
// パスワード・パスフレーズは ssh / sudo が端末から直に読むので、この画面には届かない。

import (
	"fmt"
	"strings"

	"github.com/jiikko/dotfiles/src/termsafe"
)

// logLineMax は 1 行の上限 (文字数)。
const logLineMax = 2000

// logf は 1 行を記録する (Options.Log が無ければ何もしない)。
func (m *Model) logf(format string, args ...any) {
	if m.o.Log == nil {
		return
	}
	line := termsafe.PlainLine(fmt.Sprintf(format, args...))
	if r := []rune(line); len(r) > logLineMax { // ssh の stderr を全文で持つ行がある
		line = string(r[:logLineMax]) + "…"
	}
	m.o.Log(m.o.Now().Format("2006-01-02 15:04:05.000") + " " + line)
}

// screenName は今の画面の名前 (ログの文脈)。
func (m *Model) screenName() string {
	switch {
	case m.sc == scSetup && m.su != nil:
		return "初期設定 " + m.su.host
	case m.sc == scSettings:
		return "設定"
	}
	return "一覧"
}

// logKey は押したキーを記録する。入力欄に打った文字は「(入力)」にする。
func (m *Model) logKey(key string) {
	if m.o.Log == nil {
		return
	}
	k := key
	if (m.filtering || m.bandEdit || m.ov == ovAlias) && key != "enter" && key != "esc" {
		k = "(入力)"
	}
	where := m.screenName()
	if m.ov != ovNone {
		where += fmt.Sprintf(" 板=%d", m.ov)
	}
	if m.sc == scList {
		if h := m.cursorHost(); h != "" { // 読むだけ (current() はカーソルの添字を詰めて書き換える。毎キー呼ぶ)
			where += " 行=" + h
		}
	}
	m.logf("key %s  [%s]", k, where)
}

// argvSummary はコマンドを記録する形 (長い語は詰める: 相手の固定 IP の ssh は戻しの仕掛けを 1 語に引用して持つ)。
func argvSummary(argv []string) string {
	words := make([]string, 0, len(argv))
	for _, a := range argv {
		if len(a) > 80 {
			a = a[:60] + fmt.Sprintf("…(%d 字)", len(a))
		}
		words = append(words, a)
	}
	return strings.Join(words, " ")
}
