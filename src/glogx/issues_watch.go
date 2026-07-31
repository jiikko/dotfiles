package main

// issues viewer を開いている間だけ issue ファイルの変化を見張り、別プロセス (Claude Code /
// 別ターミナルの $EDITOR / git pull) の編集をその場で反映する。
//
// 反映の機構は既にある (reloadAfterEdit)。ここが足すのは「変わったと気づく」ことだけ。
//
// なぜポーリングか (fsnotify を入れない): 遅延の差は最大 1s で、別プロセスの編集は自分の打鍵と
// 同期しないので人間には「即時」と区別できない。対して fsnotify は watcher の生成/破棄・エディタの
// tmp+rename が生むイベントのバーストの debounce・ディレクトリ追加時の再登録を抱え、失敗モードも
// 重い (fd リーク / NFS で無音)。ポーリングは取りこぼしが原理的に無い (次の周期で必ず気づく)。
//
// ⚠️ フレーム tick (spinnerActive) に混ぜない: 混ぜると viewer を開いている間ずっと 12.5fps で
// 起きることになり、「動くものがある間だけ tick を回す」という glogx の設計を崩す。autobuildWatch と
// 同じく、自分の周期で自己再アームする独立チェーンにして viewer を閉じたら止める。

import (
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// issuesWatchInterval は見張りの周期。人間が「即時」と感じる範囲で最も安いところ。
const issuesWatchInterval = time.Second

// issuesWatchMsg は 1 周期分の観測結果 (指紋)。計算は Cmd の goroutine 側で行う。
type issuesWatchMsg struct{ fp string }

// issuesWatch は見張りの状態。zero value は「見張っていない」。
type issuesWatch struct {
	seen    string // 反映済みの指紋 ("" = 次の観測を基準にする)
	pending string // 変化を検出したが、書きかけを避けるため安定を待っている指紋
	armed   bool   // チェーンが 1 本生きている (二重に張らない。maybeTick と同じ single-flight)
}

// watchCmd は次の観測を予約する。viewer を閉じている / 既に張っているなら nil (tick を増やさない)。
//
// 観測対象のパスは値で捕捉する: Issue はポインタで共有されるので、goroutine から構造体を読むと
// View 側の読み取りと競合する (scanCmd と同じ規律)。
func (v *issuesView) watchCmd() tea.Cmd {
	if v.watch.armed || !v.shown {
		return nil
	}
	v.watch.armed = true
	dirs, paths := v.watchTargets()
	return tea.Tick(issuesWatchInterval, func(time.Time) tea.Msg {
		return issuesWatchMsg{fp: issuesFingerprint(dirs, paths)}
	})
}

// watchTargets は指紋の対象を値のコピーで返す (goroutine へ渡すため)。
func (v *issuesView) watchTargets() (dirs, paths []string) {
	dirs = append([]string(nil), v.dirs...)
	paths = make([]string, 0, len(v.all)+1)
	for _, iss := range v.all {
		paths = append(paths, iss.Path)
	}
	if p := issuePath(v.open); p != "" {
		// 一覧の集合に含まれるが、本文だけ変わったケースを最短で拾うため個別にも見る
		paths = append(paths, p)
	}
	return dirs, paths
}

// handleWatch は観測結果を受けて、必要なら取り直しの Cmd を返す (常に次の観測を予約する)。
//
// 「変化を検出した次の周期でも同じ指紋なら読む」= 書きかけのファイルを掴まない。エディタの
// tmp+rename なら atomic だが、`>>` で追記するツール (シェルスクリプト・ログ追記) もあるため。
func (v *issuesView) handleWatch(msg issuesWatchMsg) tea.Cmd {
	v.watch.armed = false // この tick を消費した (継続は下の watchCmd で単一に保つ)
	if !v.shown {
		v.watch = issuesWatch{} // 閉じたら見張りごと畳む
		return nil
	}
	switch {
	case v.watch.seen == "":
		v.watch.seen = msg.fp // 開いた直後・取り直し直後: この観測を基準にする (読み直さない)
	case msg.fp == v.watch.seen:
		v.watch.pending = "" // 変化なし (安定待ちだったものが元へ戻った場合も含む)
	case msg.fp != v.watch.pending:
		v.watch.pending = msg.fp // 変化を検出。安定を確かめるまで読まない
	case v.reloadDeferred():
		// 指紋は安定したが今は読めない (下記)。pending を保って次の周期で再挑戦する
	default:
		v.watch.seen, v.watch.pending = msg.fp, ""
		return tea.Batch(v.reloadAfterEdit(), v.watchCmd())
	}
	return v.watchCmd()
}

// reloadDeferred は「変化に気づいたが今は反映しない」状態か。
//
//   - URL ピッカーは本文の URL 集合を握っているので、下から差し替えると選択が別 URL に化ける
//   - 引き出しの開閉アニメ中は、着地までレイアウトが動いている
//
// どちらも数百 ms で終わるので、次の周期で反映される。
func (v *issuesView) reloadDeferred() bool {
	return v.urlPick.active || v.drawer.animating(timeNow())
}

// issuesFingerprint は監視対象の状態を 1 本の文字列にする。
//
// ディレクトリは mtime だけ (ファイルの追加・削除・done/ への移動で動く)。⚠️ ディレクトリの mtime
// では本文の書き換えを検出できない (create/delete/rename でしか動かない) ので、ファイルは
// mtime + サイズまで見る。ハッシュにしないのは、比較しかしないので生の文字列で足りるため。
func issuesFingerprint(dirs, paths []string) string {
	var b strings.Builder
	b.Grow(len(dirs)*32 + len(paths)*48)
	for _, dir := range dirs {
		b.WriteString(dir)
		b.WriteByte(':')
		if st, err := os.Stat(dir); err == nil {
			b.WriteString(strconv.FormatInt(st.ModTime().UnixNano(), 10))
		}
		b.WriteByte('\n')
	}
	for _, path := range paths {
		b.WriteString(path)
		b.WriteByte(':')
		if st, err := os.Stat(path); err == nil {
			b.WriteString(strconv.FormatInt(st.ModTime().UnixNano(), 10))
			b.WriteByte(':')
			b.WriteString(strconv.FormatInt(st.Size(), 10))
		}
		b.WriteByte('\n')
	}
	return b.String()
}
