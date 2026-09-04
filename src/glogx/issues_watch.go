package main

// issues viewer を開いている間だけ issue ファイルの変化を見張り、別プロセス (Claude Code /
// 別ターミナルの $EDITOR / git pull) の編集をその場で反映する。
//
// 反映の機構は既にある (reloadAfterEdit)。ここが足すのは「変わったと気づく」ことだけ。
//
// 方式は「イベントで起こし、指紋で判定する」(issue 035):
//
//   - fsnotify (Linux=inotify / macOS=kqueue) のイベントで起こす。定期 wakeup が消え、反応も速い
//   - 🚨 イベントは真偽の正本にしない。1 回の保存で Create/Rename/Write が連続する・エディタの
//     tmp+rename で watch 対象の inode が入れ替わる・NFS で無音になる、と嘘をつくため。起こされたら
//     必ず指紋 (mtime + サイズ) を取り直し、本当に変わったときだけ読む
//   - 保険として低頻度 (30s) のポーリングも回す。イベントを取りこぼしても必ず追いつく。
//     fsnotify を作れない環境ではこれが唯一の経路になるので、その場合だけ周期を上げる
//
// 🚨 フレーム tick (spinnerActive) に混ぜない: 混ぜると viewer を開いている間ずっと 12.5fps で
// 起きることになり、「動くものがある間だけ tick を回す」という glogx の設計を崩す。autobuildWatch と
// 同じく、自分の周期で自己再アームする独立チェーンにして viewer を閉じたら止める。

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
	"glogx/issues"
)

const (
	// issuesWatchDebounce はイベントのバーストを畳む時間 (静まるまで吸ってから 1 回だけ測る)。
	issuesWatchDebounce = 200 * time.Millisecond
	// issuesWatchIdlePoll はイベントの取りこぼしに備えた保険の周期。
	issuesWatchIdlePoll = 30 * time.Second
	// issuesWatchBlindPoll は fsnotify を作れなかったときの周期 (イベントが来ないので唯一の経路)。
	issuesWatchBlindPoll = time.Second
	// issuesWatchVerifyPoll は「変化を見つけた後」の確認周期。安定の確認と、反映を保留した
	// (URL ピッカー・引き出しのアニメ) 後の再挑戦を兼ねる。
	issuesWatchVerifyPoll = 300 * time.Millisecond
)

// issuesWatchMsg は 1 回分の観測結果 (指紋)。計算は Cmd の goroutine 側で行う。
// closed は「イベントの経路が閉じた」(viewer を閉じた / watcher が死んだ)。
type issuesWatchMsg struct {
	fp     string
	closed bool
	// fromEvent は発行元がイベント経路か (false = 保険のポーリング)。🚨 受け取り側は「届けた
	// チェーンの札だけ」を降ろすのにこれを使う。両方降ろすと、まだ w.Events でブロックしている
	// goroutine が居るのに evArmed が false になり、single-flight をすり抜けて 2 本目が張られる
	// (観測 1 回につき goroutine が 1 本ずつ積み上がる)。
	fromEvent bool
	// gen は観測を発行した世代。閉じ → 開き直しで増える (stopWatch)。🚨 これが無いと、閉じる前に
	// 張った古いチェーンの closed が、開き直して作った**新しい** watcher を閉じてしまう
	// (以降イベントが来ずポーリングだけに縮退する。無音ではないが即時性を静かに失う)。
	gen int
}

// dirWatcher は fsnotify.Watcher のうち見張りが使う面だけを切った seam (issue の見張りと
// git log の見張り (gitlog_watch.go) で共用する)。
//
// 🚨 実装を差し替えるためではなく、**CI で不変条件を観測できるようにするため**にある。
// CI (ubuntu-slim) では fsnotify.NewWatcher が通らず watch.w が nil になるので、実 watcher を
// 前提にしたテストはすべて skip され、「消えて戻ったディレクトリを再 Add する」のような配線の
// 退行が CI では一度も検査されない (issue 087)。フェイクを差せば実 fsnotify 無しで startWatch /
// eventCmd の本体をそのまま走らせられる。
// Events/Errors をメソッドにしているのは fsnotify.Watcher がチャネルを**フィールド**で公開して
// いてインタフェースに乗らないため (fsWatcher が薄く包む)。
type dirWatcher interface {
	Add(string) error
	Close() error
	WatchList() []string
	Events() <-chan fsnotify.Event
	Errors() <-chan error
}

// fsWatcher は *fsnotify.Watcher を dirWatcher へ合わせるだけのアダプタ (ロジックを持たない)。
type fsWatcher struct{ w *fsnotify.Watcher }

func (f fsWatcher) Add(dir string) error          { return f.w.Add(dir) }
func (f fsWatcher) Close() error                  { return f.w.Close() }
func (f fsWatcher) WatchList() []string           { return f.w.WatchList() }
func (f fsWatcher) Events() <-chan fsnotify.Event { return f.w.Events }
func (f fsWatcher) Errors() <-chan error          { return f.w.Errors }

// newDirWatcher は watcher を作る唯一の経路 (テストがフェイクへ差し替える口)。
//
// 🚨 production ではここを分岐させない。差し替えはテストだけの都合。
// 🚨 差し替えは package 変数の書き換えなので、この seam を使うテストで t.Parallel() を呼ばないこと
// (-race が「テスト基盤のデータレース」として落ち、検証対象と無関係な形で失敗する)。
// 🚨 包む前に nil を弾く: interface に nil ポインタを入れると `w == nil` が false になり
// (typed nil)、「watcher を作れない環境ではポーリングへ縮退する」という startWatch / eventCmd /
// pollInterval / handleWatch の nil ガード全部が panic に変わる。fsnotify v1.10.1 は成功時に
// 必ず非 nil を返すので今は起きないが、その契約 1 つに縮退の正しさを乗せない。
var newDirWatcher = func() (dirWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, errors.New("fsnotify.NewWatcher が nil を返した")
	}
	return fsWatcher{w: w}, nil
}

// issuesWatch は見張りの状態。zero value は「見張っていない」。
type issuesWatch struct {
	w dirWatcher
	// 🚨 「Add 済み」を自前で覚えて skip しないこと。fsnotify の watch は**ディレクトリが
	// 消えると黙って失われる**ので、印だけが残って二度と Add されない状態になる (実測
	// 2026-08-21: 実 repo で git switch により issues/done が消えて戻ると、同一 viewer
	// セッション中は done/ 内の変更が恒久的に無音。手動の取り直し 3 回でも復帰せず、
	// 指紋ポーリング 30s も done/ の未知の新規ファイルは見ないので代替にならない)。
	// Add は冪等 (同一パスの重複配送は起きないことを実測) なので、毎回無条件に Add する。
	gen int // 見張りの世代 (閉じるたびに増える。古いチェーンの観測を弾く)

	seen    string // 反映済みの指紋 ("" = 次の観測を基準にする)
	pending string // 変化を検出したが、書きかけを避けるため安定を待っている指紋

	// チェーンは 2 本 (イベント待ち / 保険のポーリング)。それぞれ二重に張らない
	// (maybeTick と同じ single-flight)。
	evArmed   bool
	pollArmed bool
}

// watchCmd は次の観測を予約する (イベント待ち + 保険のポーリング)。viewer を閉じている /
// 既に張っているチェーンは nil を返して増やさない。
//
// 観測対象のパスは値で捕捉する: Issue はポインタで共有されるので、goroutine から構造体を読むと
// View 側の読み取りと競合する (scanCmd と同じ規律)。
func (v *issuesView) watchCmd() tea.Cmd { return tea.Batch(v.eventCmd(), v.pollCmd()) }

// startWatch は監視対象のディレクトリを fsnotify へ登録する (スキャン結果が届くたびに呼ぶ)。
//
// fsnotify は再帰しないので、issue ディレクトリと「issue ファイルが実際に居るサブディレクトリ」
// (done/ pending/ とサブグループ) を個別に Add する。epic/ と全 group dir は空でも登録する。
// 新しいサブディレクトリの作成は親のイベントとして飛び、取り直し → ここで Add されて追従する。
// watcher を作れない環境 (fd 上限・未対応 platform) では黙ってポーリングだけに縮退する。
func (v *issuesView) startWatch() {
	if !v.shown {
		return
	}
	if v.watch.w == nil {
		w, err := newDirWatcher()
		if err != nil {
			return
		}
		v.watch.w = w
	}
	for _, dir := range v.watchDirs() {
		// 失敗は無視して次の取り直しで再挑戦する (消えたディレクトリ等)。成功しても印を
		// 残さない = 消えて戻ったディレクトリが再び watch される (上のフィールドコメント参照)。
		_ = v.watch.w.Add(dir)
	}
}

// stopWatch は watcher を閉じて状態を畳む (fd を残さない)。世代を 1 つ進めて、閉じる前に
// 張ってあったチェーンの観測が開き直した後の状態へ効かないようにする。
func (v *issuesView) stopWatch() {
	if v.watch.w != nil {
		_ = v.watch.w.Close()
	}
	v.watch = issuesWatch{gen: v.watch.gen + 1}
}

// watchDirs は fsnotify へ Add するディレクトリ (issue ディレクトリ + ファイルが居るサブ + epic/ と
// 全 group dir)。空の group dir も含めるのは、そこへ最初の issue が置かれた変化を拾うため。
func (v *issuesView) watchDirs() []string {
	dirs, _ := v.watchTargets()
	return dirs
}

// eventCmd は fsnotify のイベントを 1 回待ち、バーストを畳んでから指紋を取る。
//
// ブロックする Cmd にするのは、外から Msg を送る口 (Program ハンドル) を持たずに済むため
// (bubbletea では Cmd の goroutine で待つのが定石)。viewer を閉じると watcher が Close され、
// チャネルが閉じてこの Cmd も終わる。
func (v *issuesView) eventCmd() tea.Cmd {
	if v.watch.evArmed || !v.shown || v.watch.w == nil {
		return nil
	}
	v.watch.evArmed = true
	w, gen := v.watch.w, v.watch.gen
	dirs, paths := v.watchTargets()
	return func() tea.Msg {
		select {
		case _, ok := <-w.Events():
			if !ok {
				return issuesWatchMsg{closed: true, fromEvent: true, gen: gen}
			}
		case _, ok := <-w.Errors():
			if !ok {
				return issuesWatchMsg{closed: true, fromEvent: true, gen: gen}
			}
			// エラーは握って観測へ倒す (指紋が正本なので、測り直せば辻褄は合う)
		}
		drainWatchEvents(w, issuesWatchDebounce)
		return issuesWatchMsg{fp: issuesFingerprint(dirs, paths), fromEvent: true, gen: gen}
	}
}

// drainWatchEvents は quiet の間イベントが来なくなるまで吸う (バーストの畳み込み)。
func drainWatchEvents(w dirWatcher, quiet time.Duration) {
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-w.Events():
			if !ok {
				return
			}
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(quiet) // まだ書いている: 静まるまで待つ
		case <-w.Errors():
		case <-timer.C:
			return
		}
	}
}

// pollCmd は保険のポーリング。イベントを取りこぼしても必ず追いつく。
func (v *issuesView) pollCmd() tea.Cmd {
	if v.watch.pollArmed || !v.shown {
		return nil
	}
	v.watch.pollArmed = true
	gen := v.watch.gen
	dirs, paths := v.watchTargets()
	return tea.Tick(v.pollInterval(), func(time.Time) tea.Msg {
		return issuesWatchMsg{fp: issuesFingerprint(dirs, paths), gen: gen}
	})
}

// pollInterval は保険の周期。変化を見つけた後は安定の確認と保留の再挑戦のために短くし、
// イベントが来ない環境 (watcher を作れなかった) では唯一の経路になるので上げる。
func (v *issuesView) pollInterval() time.Duration {
	switch {
	case v.watch.pending != "":
		return issuesWatchVerifyPoll
	case v.watch.w == nil:
		return issuesWatchBlindPoll
	default:
		return issuesWatchIdlePoll
	}
}

// watchTargets は指紋の対象を値のコピーで返す (goroutine へ渡すため)。
func (v *issuesView) watchTargets() (dirs, paths []string) {
	return issuesWatchDirs(v.dirs, v.all), issuesWatchPaths(v.all)
}

// issuesWatchDirs は scanIssues と watchTargets が共有するディレクトリ集合を作る。epic の group
// は空でも含めるので、まだ issue が 1 件もない group の最初のファイル作成を検出できる。
func issuesWatchDirs(baseDirs []string, all []*issues.Issue) []string {
	seen := make(map[string]bool, len(baseDirs)+len(all)+4)
	out := make([]string, 0, len(baseDirs)+len(all)+4)
	add := func(dir string) {
		if dir != "" && !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	for _, dir := range baseDirs {
		add(dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		epicName := ""
		for _, e := range entries {
			if e.IsDir() && strings.EqualFold(e.Name(), issues.EpicDirName) {
				epicName = e.Name()
				break
			}
		}
		if epicName == "" {
			continue
		}
		epic := filepath.Join(dir, epicName)
		add(epic)
		entries, err = os.ReadDir(epic)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && e.Type()&os.ModeSymlink == 0 {
				groupDir := filepath.Join(epic, e.Name())
				add(groupDir)
				// next/ (および規約上迷子になる done/pending/) が先に作られて
				// 空でも、そこへ最初の md が入ったことを拾う。groupDir 自体は
				// non-recursive watcher なので、子ディレクトリも直接見張る。
				groupEntries, groupErr := os.ReadDir(groupDir)
				if groupErr != nil {
					continue
				}
				for _, child := range groupEntries {
					if !child.IsDir() || child.Type()&os.ModeSymlink != 0 {
						continue
					}
					if _, ok := issues.EpicChildStatus(child.Name()); ok {
						add(filepath.Join(groupDir, child.Name()))
					}
				}
			}
		}
	}
	for _, iss := range all {
		add(filepath.Dir(iss.Path))
	}
	return out
}

// issuesWatchPaths は指紋の対象ファイルを組み立てる。
//
// 🚨 スキャン側 (基準を取る scanIssues) と観測側 (変化を見る watchTargets) で必ず同じ集合を
// 作ること。食い違うと、取り直すたびに基準が集合 A・観測が集合 B で決まって永久に一致せず、
// 何も変わっていないのに観測のたびに取り直しが回り続ける (取り直しがまた基準を作るので止まらない)。
//
// 開いている本文を個別に足さないのはそのため: パスは v.all に含まれており、二重に入れると
// スキャン側 (v.open を知らない) と食い違う。ファイルごと消えた場合の再生成はディレクトリの
// mtime で拾える。
func issuesWatchPaths(all []*issues.Issue) []string {
	paths := make([]string, 0, len(all))
	for _, iss := range all {
		paths = append(paths, iss.Path)
	}
	return paths
}

// handleWatch は観測結果を受けて、必要なら取り直しの Cmd を返す (常に次の観測を予約する)。
//
// 「変化を検出した次の周期でも同じ指紋なら読む」= 書きかけのファイルを掴まない。エディタの
// tmp+rename なら atomic だが、`>>` で追記するツール (シェルスクリプト・ログ追記) もあるため。
func (v *issuesView) handleWatch(msg issuesWatchMsg) tea.Cmd {
	if msg.gen != v.watch.gen {
		return nil // 閉じる前に張った古いチェーンの観測 (札も触らない)
	}
	if msg.closed {
		v.watch.evArmed = false
		if !v.shown {
			v.stopWatch()
			return nil
		}
		// watcher が死んだ (fd 回収・NFS 等)。閉じてポーリングだけで続ける (無音にはしない)
		if v.watch.w != nil {
			_ = v.watch.w.Close()
			v.watch.w = nil
		}
		return v.watchCmd()
	}
	// 指紋つきの観測はイベント経路とポーリング経路の両方から来る。🚨 降ろすのは**届けた
	// チェーンの札だけ**にする (closed 経路と同じ規律)。両方降ろすと、まだ w.Events で
	// ブロックしている goroutine が生きているのに evArmed が false になり、watchCmd の
	// single-flight をすり抜けて 2 本目が張られる = 観測 1 回ごとに goroutine が 1 本残る。
	// 平常時のポーリングは 30s 周期なので、viewer を開きっぱなしにするだけで増え続ける。
	if msg.fromEvent {
		v.watch.evArmed = false
	} else {
		v.watch.pollArmed = false
	}
	if !v.shown {
		v.stopWatch() // 閉じたら watcher ごと畳む (fd を残さない)
		return nil
	}
	switch {
	case v.watch.seen == "":
		// 基準がまだ無い = スキャン結果より先に観測が届いた (初回スキャン中) / スキャンが
		// 失敗した。通常はスキャン自身が基準を持ってくる (receive) のでここは通らない。
		v.watch.seen = msg.fp
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
// どちらも数百 ms で終わるので、pollInterval が短くなって次の周期で反映される。
func (v *issuesView) reloadDeferred() bool {
	return v.urlPick.active || v.drawer.animating(timeNow())
}

// issuesFingerprint は監視対象の状態を 1 本の文字列にする。
//
// ディレクトリは mtime だけ (ファイルの追加・削除・done/ への移動で動く)。🚨 ディレクトリの mtime
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
		if strings.EqualFold(filepath.Base(filepath.Clean(dir)), issues.EpicDirName) {
			b.WriteString(dir)
			b.WriteString(":entries:")
			if entries, err := os.ReadDir(dir); err == nil {
				for _, entry := range entries {
					b.WriteString(entry.Name())
					if entry.IsDir() {
						b.WriteString("/")
					}
					b.WriteByte('\x00')
				}
			}
			b.WriteByte('\n')
		}
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
