package main

import (
	"math"
	"time"

	"glogx/internal/gitdata"

	"glogx/internal/app"

	tea "charm.land/bubbletea/v2"
)

// pushMsg は git push の実行結果 (b → y 確認後)。
type pushMsg struct{ err error }

// noPromptGitCmd は remote に触る git (push/pull) 用のコマンドを組む。GIT_TERMINAL_PROMPT=0
// で「認証情報が要るのに helper が無い」場合に /dev/tty へ対話プロンプトを出させず即エラーに
// する: bubbletea が同じ端末を raw mode で握っているため、git が tty を奪うと表示が壊れ入力
// 挙動が未定義になる (対話認証は TUI の外でやるべき作業)。タイムアウトは付けない — 正当な
// 巨大 push が遅い回線で中断される方が push 失敗として有害なため (レビュー K2)。
// pullMsg は git pull --rebase の実行結果 (u → y 確認後)。glogx の独自機能。
type pullMsg struct{ err error }

// pushPlan は b の件数の出典 (gitdata.PlanPush)。テストで実 git を叩かないための差し替え点。
var pushPlan = gitdata.PlanPush

func (m *browseModel) onPull(msg pullMsg) (tea.Model, tea.Cmd) {
	m.actModal.pulling = false
	if msg.err != nil {
		m.showGitFailure("pull に失敗", msg.err)
		return m, m.maybeTick()
	}
	// 成功トーストを右下にせり上げつつ全面リロード (アニメで画面が動いてもトーストは数秒残る)。
	m.toast.Show("pull --rebase しました", true)
	// pull で自分のソースが更新されたなら、その場で裏ビルドを始める (autobuildAfterPull)。
	// status viewer を開いたまま p で pull した場合は、その場で読み直す: 自動更新は 1.5 秒
	// 周期なので、待たせるとヘッダーの ahead/behind が古いまま残って「効いていない」に見える。
	return m, tea.Batch(m.reloadAfterPull(), m.maybeTick(), m.autobuildAfterPull(), m.statusOv.LoadCmd())
}

func (m *browseModel) onPush(msg pushMsg) (tea.Model, tea.Cmd) {
	m.actModal.pushing = false
	if msg.err != nil {
		m.showGitFailure("push に失敗", msg.err)
		return m, m.maybeTick()
	}
	m.toast.Show("push しました", true)
	if len(m.commits) == 0 {
		return m, m.maybeTick() // 再取得先が無くてもトーストは出す (アニメ tick を回す)
	}
	if !m.hasRepo {
		// CI の再取得は無いが、未 push (🔼) は手元で判定し直す (読み直さないと push 済みのコミットが 🔼 のまま残る)
		cmds := []tea.Cmd{m.reloadLog(true, "push 後の再読込に失敗しました: "), m.maybeTick()}
		if m.statusOv.Visible() {
			cmds = append(cmds, m.statusOv.LoadCmd()) // GitHub の repo のときと同じく viewer の ahead を即消す
		}
		return m, tea.Batch(cmds...)
	}
	// status viewer 表示中は演出を出さない (対象のコミット一覧が画面に無く、演出分だけ
	// 再取得が遅れるだけ)。代わりに viewer を読み直してヘッダーの ahead を即消す
	// (pull 成功時の statusOv.loadCmd と同じ理由)
	if m.statusOv.Visible() {
		return m, tea.Batch(m.refetchAfterPush(), m.maybeTick(), m.statusOv.LoadCmd())
	}
	if m.startPushAnim() {
		return m, m.maybeTick() // 演出完了後 (advancePushAnim) に refetchAfterPush へ進む
	}
	return m, tea.Batch(m.refetchAfterPush(), m.maybeTick())
}

// confirmPush は push 確認 (y/N) に入る。未 push が 1 件も無ければ確認を出さない
// (誤爆防止と「push 済みなのに聞かれる」違和感の回避)。
func (m *browseModel) confirmPush() tea.Cmd {
	// 🚨 件数は表示中の statuses から数えない: path filter (`glogx -- <path>`)・-n・GitHub を解決できない repo では、
	// 画面に出ていない / CI の状態を持たない未 push があり、0 件と数えて push を断っていた。git push が送るものを見積もる。
	// 断るのは見積もりが確かで 0 件のときだけ (gitdata.PushPlan の doc)
	plan := pushPlan()
	if plan.Exact && plan.N == 0 {
		// 「押しても何も起きない理由」の通知はキー待ちのモーダルでなく右下トーストで出す
		// (ユーザー要望 2026-07-25)。他の no-op 通知 (コピー対象なし・PR なし等) と同じ経路。
		m.toast.Show("未 push のコミットはありません", false)
		return m.maybeTick()
	}
	m.actModal.pushConfirm, m.actModal.pushPlan = true, plan
	return nil
}

// reloadAfterPull は pull --rebase 成功後の全面リロード (カーソルは新規コミットの先頭へ)。
func (m *browseModel) reloadAfterPull() tea.Cmd {
	return m.reloadLog(false, "pull 後の再読込に失敗しました: ")
}

// logData は git log の読み直しで取り直すもの一式 (Update の外で集め、applyLogData でモデルへ入れる)。
type logData struct {
	commits  []gitdata.Commit
	raw      []string // 表示用 verbatim (oneline では nil)
	statuses map[string]gitdata.CIState
	toFetch  []string
	repo     gitdata.Repo
	hasRepo  bool
}

// loadLogData は git を fork して logData を集める純粋な読み込み (モデルを触らない)。
// git を 5-6 本 fork する (LoadCommits / LoadLogDisplay / planStatuses の rev-list・remote get-url・
// rev-parse) ので、Update の中では呼ばず Cmd に出す (reflectGitLogChange)。pull 後の全面リロード
// (reloadLog) は利用者の操作なので同期のまま。
func loadLogData(opts *Options, colored, oneline bool) (logData, error) {
	commits, err := gitdata.LoadCommits(opts.LogQuery(), colored)
	if err != nil {
		return logData{}, err
	}
	shas := make([]string, len(commits))
	for i, c := range commits {
		shas[i] = c.SHA
	}
	d := logData{commits: commits}
	d.statuses, d.toFetch, d.repo, d.hasRepo, _ = planStatuses(opts, shas)
	if !oneline {
		if raw, dispErr := gitdata.LoadLogDisplay(opts.LogQuery(), colored); dispErr == nil {
			d.raw = raw // 照合失敗は applyLogData 側で nil = 自前レンダリングへ
		}
	}
	return d, nil
}

// reloadLog は git log を読み直して派生キャッシュを畳む共通経路 (同期。pull / push の後の全面リロード用)。
// 読み込みと反映は loadLogData / applyLogData に分かれており、外部変更の追従 (gitlog_watch.go) は
// 読み込みだけを Cmd へ出して同じ applyLogData で反映する。
// 読み直しに失敗したらモデルを一切触らず、警告はここで出す (呼び出し側は追加の通知をしない)。
// keepView は applyLogData の doc (pull は false = 先頭へ寄せて新しいコミットを見せる。push は true = 見ている画面を動かさない:
// 送ったコミットは同じなので、押した人が見ていた位置から動かす理由が無い)。
func (m *browseModel) reloadLog(keepView bool, failPrefix string) tea.Cmd {
	d, err := loadLogData(m.opts, m.colored, m.oneline)
	if err != nil {
		m.showWarning(failPrefix + app.FirstLine(err.Error()))
		return nil
	}
	_, cmd := m.applyLogData(d, keepView)
	return cmd
}

// applyLogData は読み直した logData をモデルへ入れ、派生キャッシュを畳む。rebase でローカル SHA が
// 変わりうるため、コミット列・push 状態・CI・派生キャッシュ (details/PR/diff/job 詳細)
// をすべて取り直す (部分更新は旧 SHA の残骸が混ざる)。
//
// keepView == false なら先頭へ寄せて新規コミット行を上から降らせる (pull した本人は先頭を見る)。
// true なら**見えている画面を動かさない** (外部の変更を反映するとき、読んでいる行をずらさない
// = gitlog_watch.go)。錨は 2 段に取る:
//
//   - ビューポート先頭のコミット → その画面行を保つ (offset の復元)
//   - カーソルのコミット → カーソルの選択を保つ
//
// 🚨 カーソルだけを錨にしないこと: ctrl+d でページ送りした状態 (cursor == 0 のまま下を読む) では
// カーソルが指すのは**先頭コミット**で、`--amend` で最も消えやすい SHA になる。消えた瞬間に
// 「先頭へ倒す」経路へ落ちて読んでいた位置が飛ぶ (2026-09-01 に実測)。
// 両方消えていたら (rebase / reset) 先頭へ倒す。
//
// 錨は行集合が入れ替わる直前 (この関数の中) で測る。非同期の読み直しでは Cmd を出してから
// 結果が届くまでに利用者がスクロールしうるので、Cmd を出す時点で測った錨は使えない。
func (m *browseModel) applyLogData(d logData, keepView bool) (added int, cmd tea.Cmd) {
	commits := d.commits
	// 読み直し前の SHA 集合。先頭へ増えた新規コミット数を「既知 SHA に当たるまで」で数え、
	// アニメーションの対象行数を決める (rebase で SHA が書き換わった場合も破綻しない)。
	oldSHAs := make(map[string]struct{}, len(m.commits))
	for _, c := range m.commits {
		oldSHAs[c.SHA] = struct{}{}
	}
	cursorSHA, topSHA, topRow := "", "", 0
	if keepView {
		if m.cursor >= 0 && m.cursor < len(m.commits) {
			cursorSHA = m.commits[m.cursor].SHA
		}
		if idx := topVisibleCommitIdx(m.lines(), m.offset); idx >= 0 && idx < len(m.commits) {
			topSHA = m.commits[idx].SHA
			topRow = headerLineIndex(m.lines(), idx) - m.offset
		}
	}
	m.commits = commits
	m.statuses, m.toFetch, m.repo, m.hasRepo = d.statuses, d.toFetch, d.repo, d.hasRepo
	m.details = map[string][]gitdata.CheckDetail{}
	m.detailsLoading, m.detailsWaiting = map[string]bool{}, map[string]bool{}
	m.detailOv.Reset() // job 詳細ログキャッシュも破棄 (旧 SHA の残骸を持ち越さない)
	m.prCache = map[string]*gitdata.PRRef{}
	m.prBusy = map[string]bool{}
	m.prStatusOv.Reset() // 旧 SHA の PR 詳細キャッシュも破棄
	m.diffOv.Reset()
	m.closePanel()
	m.awaitCI, m.awaitAttempts = nil, 0
	m.ciPollGen++       // 旧世代の残タイマーを無効化する (リロードで対象そのものが入れ替わる)
	m.ciPolling = false // 次の ciResultMsg 着弾で張り直す
	// 🚨 ciPollInFlight はここで false に戻さない: pull で SHA 集合が入れ替わっても既存 SHA は
	// 残るので、飛んでいる旧 poll と新 poll が同一 SHA を並行取得し、完了順で古い結果が勝ちうる。
	// 旧 poll の結果が着弾した時点で false に戻る (数周期 fetch を見送るだけで追従は途切れない)。
	m.glide.Stop() // リロードの演出はアニメ側が担うので一覧の glide は破棄
	m.verbatim = nil
	if !m.oneline && d.raw != nil {
		m.verbatim = VerbatimLines(d.raw, commits) // 照合失敗は nil = 自前レンダリングへ
	}
	m.invalidateLines()
	// 見張りの基準を作り直す (gitlog_watch.go)。自分で読み直した後の状態を「変化」として
	// もう一度反映しないため。空にすると次の測定が手元のコミット列と突き合わせて基準を作る。
	// 🚨 飛んでいる測定の札も降ろす: 読み直しの直前に測った古い指紋が届くと、新しいコミット列と
	// 突き合わせて必ず不一致になり、無駄な再読込・トースト・CI 再取得が続けて走る。
	// reloadSeq を進めるのは、飛んでいる非同期の読み直し (reflectGitLogChange) を捨てるため:
	// この後に届く古い logData を入れると、いま入れたものより古い状態へ戻る。
	m.logWatch.hasSeen, m.logWatch.measuring = false, false
	m.logWatch.reloadSeq++
	m.logWatch.reloading = false
	added = countNewCommits(commits, oldSHAs)
	newTop, newCursor := indexOfCommit(commits, topSHA), indexOfCommit(commits, cursorSHA)
	switch {
	case newTop >= 0:
		// 画面先頭のコミットが残っている: その画面行を保つ = 見えている内容が動かない。
		// 新規コミットは画面外の上に積まれるので降らせる演出はしない。
		m.offset = m.clampOffset(headerLineIndex(m.lines(), newTop) - topRow)
		if newCursor >= 0 {
			m.cursor = newCursor
		} else {
			m.cursor = app.ClampIdx(newTop, len(commits)) // カーソルのコミットだけ消えた (amend 等)
		}
	case newCursor >= 0:
		// 画面先頭は消えたがカーソルのコミットは残っている: 選択を保って画面内へ収める
		m.cursor = newCursor
		m.ensureCursorVisible()
	default:
		m.cursor, m.offset = 0, 0 // カーソルは新規コミットの先頭へ (ユーザー要望 2026-07-20)
		// 先頭に増えた新規コミット行を上から降らせる演出。startPullAnim が offset を新規行数だけ
		// 手前 (下スクロール位置) へ置き、tick で 1 行/フレームずつ 0 へ戻すと新規行が上から入り
		// 既存行が下へずれて見える。アニメしないと決まったときだけ即カーソル可視化に落とす
		m.startPullAnim(oldSHAs)
		if !m.pullAnimating {
			m.ensureCursorVisible()
		}
	}

	if !m.hasRepo || len(m.toFetch) == 0 {
		m.pendingFetches = 0 // 取得を始めないので下ろす (fetching() もこれで下りる)
		if m.pullAnimating {
			return added, m.maybeTick() // CI 取得は無いが、アニメーションのために tick を回す
		}
		return added, nil
	}
	return added, tea.Batch(m.startCIFetch(m.toFetch), m.maybeTick())
}

// indexOfCommit は sha のコミットの index (無ければ -1)。sha == "" は常に -1 (錨なし)。
func indexOfCommit(commits []gitdata.Commit, sha string) int {
	if sha == "" {
		return -1
	}
	for i, c := range commits {
		if c.SHA == sha {
			return i
		}
	}
	return -1
}

// countNewCommits は先頭へ増えた新規コミット数 (既知 SHA に当たるまで)。
func countNewCommits(commits []gitdata.Commit, oldSHAs map[string]struct{}) int {
	n := 0
	for _, c := range commits {
		if _, ok := oldSHAs[c.SHA]; ok {
			break // 既知コミットに到達 = ここから下は元からある分
		}
		n++
	}
	return n
}

// pullAnimMaxLines は pull アニメで降らせる最大行数。大量 pull でも待ちが伸びすぎないよう
// 「頭だけ」流し、超過分は最初から所定位置に置く (ユーザー要望 2026-07-20)。
const pullAnimMaxLines = 8

// startPullAnim は pull 後に先頭へ増えた新規コミットの行数を求め、あれば offset を
// その分 (上限 pullAnimMaxLines) だけ下げてアニメーションを開始する。tick は
// reloadAfterPull / tickMsg 側で回す。
func (m *browseModel) startPullAnim(oldSHAs map[string]struct{}) {
	newCommits := countNewCommits(m.commits, oldSHAs)
	if newCommits == 0 {
		return
	}
	// 新規コミットが占める行数 (medium 表示では 1 コミット複数行)。最初の「既存コミット」の
	// 行 index が、そのまま先頭からの新規行数になる。全行が新規で既存が下に無いなら
	// 押し下げる相手がいないのでアニメしない (newLines が 0 のまま)
	lines := m.lines()
	newLines := 0
	for i, l := range lines {
		if l.CommitIdx >= newCommits {
			newLines = i
			break
		}
	}
	// offset スクロールで新規行を画面外上部に隠す方式のため、リスト全体が画面に収まる
	// (スクロール不能な) 短いリストではアニメできない → 即表示にフォールバック
	if newLines == 0 || len(lines) <= m.pageSize() {
		return
	}
	m.offset = min(newLines, pullAnimMaxLines)
	m.pullAnimating = true
}

// advancePullAnim は pull アニメーションを 1 行分進める (1 フレーム = tick 1 回)。
// offset を 0 に向けて減らすと、新規行が上から降りて既存行が下へずれる。
func (m *browseModel) advancePullAnim() {
	m.offset--
	if m.offset <= 0 {
		m.offset = 0
		m.pullAnimating = false
		m.ensureCursorVisible()
	}
}

// pushAnimMaxSteps は push 演出で 1 段ずつ流す最大コミット数。大量 push でも
// 待ちが伸びないよう頭打ちにし、超過分は開始時に即切り替える (pullAnimMaxLines と同型)。
const pushAnimMaxSteps = 8

// pushAnimStep は境界が 1 コミット上がる間隔。80ms/段では目で追えない
// (ユーザーフィードバック 2026-07-23) ため、1 段ずつ確実に視認できる速さにする。
// 🚨 2 倍速化 (2026-09-05) で 600ms → 300ms にしたが、80ms/段が「追えない」側だった以上
// ここを更に半分にするのは視認性の下限に触る。次に速くするなら段数 (pushAnimMaxSteps) を
// 減らす側を先に検討する。
const pushAnimStep = 300 * time.Millisecond

// pushSlideDuration は境界通過したコミット区画が右へ沈んで戻ってくるまでの時間。
// pushAnimStep より長いので複数コミットの push では沈み込みが波状に重なる
// (この大小関係が崩れると波状に見えないので、片方だけ変えない)。
const pushSlideDuration = 500 * time.Millisecond

// startPushAnim は push 成功の演出を開始する。未 push だったコミットを古い順に
// 1 コミット/フレームで取得中 (spinner) 表示へ切り替えていくと、insertPushBoundary の
// 境界罫線が 1 段ずつ上へスライドし、先頭サマリの ↑N が減っていき、最後に
// (all pushed ✓) へ着地する (描画側は無変更で、statuses の遷移だけで演出が成立する)。
// 未 push が無ければ何もせず false (呼び出し側が即 refetchAfterPush へ)。
func (m *browseModel) startPushAnim() bool {
	var unpushed []string // 新しい順
	for _, c := range m.commits {
		if m.statuses[c.SHA] == gitdata.StateUnpushed {
			unpushed = append(unpushed, c.SHA)
		}
	}
	if len(unpushed) == 0 {
		return false
	}
	m.pushAnimTip = unpushed[0]
	for len(unpushed) > pushAnimMaxSteps {
		last := len(unpushed) - 1
		delete(m.statuses, unpushed[last])
		unpushed = unpushed[:last]
	}
	m.invalidateLines()
	m.pushAnimating = true
	m.pushAnimNext = app.TimeNow().Add(pushAnimStep)
	return true
}

// advancePushAnim は pushAnimStep 経過ごとに push 演出を 1 コミット分進める。
// 最も古い StateUnpushed を消すと境界が 1 コミット上がる。全部消えたら演出終了で、
// 本来の後処理 (CI 全件再取得) へ進む cmd を返す。
func (m *browseModel) advancePushAnim() tea.Cmd {
	if app.TimeNow().Before(m.pushAnimNext) {
		return nil
	}
	m.pushAnimNext = app.TimeNow().Add(pushAnimStep)
	for i := len(m.commits) - 1; i >= 0; i-- {
		if m.statuses[m.commits[i].SHA] == gitdata.StateUnpushed {
			delete(m.statuses, m.commits[i].SHA)
			if m.pushSlides == nil {
				m.pushSlides = map[string]time.Time{}
			}
			m.pushSlides[m.commits[i].SHA] = app.TimeNow() // 通過した区画の沈み込みを開始
			m.invalidateLines()
			return nil
		}
	}
	m.pushAnimating = false
	return m.refetchAfterPush()
}

// slideColumns は push 演出の「origin に吸い込まれる」沈み込み: 境界が通過したコミットの
// 区画 (ヘッダー行から次のコミットまで) を、画面幅の半分まで右へ滑らせて戻す (ユーザー要望
// 2026-07-23「50%くらい右に埋まる」)。返り値は行 index → 右オフセット列数 (演出なしは nil)。
// sin カーブで 0 → 半幅 → 0 と往復し、区画の判定はヘッダー行起点で行う (罫線行は CommitIdx
// を持つがヘッダー行に後続しないため巻き込まない)。
func (m *browseModel) slideColumns(lines []Line) map[int]int {
	if len(m.pushSlides) == 0 {
		return nil
	}
	depth := m.contentWidth() / 2
	byCommit := map[int]int{}
	for i, c := range m.commits {
		start, ok := m.pushSlides[c.SHA]
		if !ok {
			continue
		}
		p := float64(app.TimeNow().Sub(start)) / float64(pushSlideDuration)
		if p < 0 || p >= 1 {
			continue
		}
		if off := int(float64(depth) * math.Sin(math.Pi*p)); off > 0 {
			byCommit[i] = off
		}
	}
	if len(byCommit) == 0 {
		return nil
	}
	cols := map[int]int{}
	cur, active := -1, 0
	for i, l := range lines {
		if l.Header {
			cur = l.CommitIdx
			active = byCommit[cur]
		} else if l.CommitIdx != cur {
			active = 0 // 区画を抜けた (罫線行や次コミットへの切り替わり)
		}
		if active > 0 {
			cols[i] = active
		}
	}
	return cols
}

// refetchAfterPush は push 後の CI 再取得。表示中リスト全体の CI 状態を破棄して取り直す
// (ユーザー要望 2026-07-19: push で CI が走り出すため、起動時キャッシュ由来の表示は丸ごと
// 古くなる)。statuses から消す → スピナー表示に戻り、toFetch 差し替えで一括取得と同じ経路
// (ciResultMsg) に乗せる。取得結果は fetched 経由で終了時に SaveCache へマージされ、
// ファイルキャッシュ側も新しい観測で上書きされる。
// ポーリング対象は push の先頭 (tip = 最新の unpushed) だけ (ユーザー要望 2026-07-19)。
// CI は push イベントの head commit にしか走らないのが普通で、途中のコミットまで対象に
// すると CI が永遠に見えず上限までスピナーが回り続ける。途中コミットの「checks なし (–)」
// は本物なので通常どおり取得・キャッシュする。tip は演出が statuses を先に消すため
// pushAnimTip (startPushAnim が捕捉) を優先する。
func (m *browseModel) refetchAfterPush() tea.Cmd {
	// 自分の push で origin/* が動いた分は見張りの基準を作り直して飲み込む (gitlog_watch.go)。
	// SHA は変わらないので次の測定は再読込せず基準だけ更新する = push の直後に「更新しました」
	// のトーストと CI 再取得が二重に走らない。🚨 %D の decoration (origin/master の位置) は
	// 読み直していないので古いまま残る (push 前からの挙動。全面リロードを自分の操作の直後に
	// 足さない判断)。飛んでいる測定も同じ理由で捨てる (reloadLog と同じ規律)。
	m.logWatch.hasSeen, m.logWatch.measuring = false, false
	m.awaitCI = map[string]bool{}
	m.awaitAttempts = 0
	if m.pushAnimTip != "" {
		// 🚨 **commits 所属を確かめてから入れる**。awaitCI の不変条件は
		// 「awaitCI ⊆ commits の SHA」で、これを破ると**どの経路でも取り除かれない**
		// 要素が残る: ciPollTargets は commits を走査して targets を組むので commits 外の
		// SHA は追従対象にならず、statuses も永久に現れないので settleAwaitCI も落とせない。
		// 結果 spinnerActive() が下りず、tickMsg の invalidateLines が 80ms ごとに
		// 全行を組み直し続ける (画面は静止しているのにアイドルへ戻らない)。
		// 踏む筋: push 演出中に u で pull → applyLogData が awaitCI を nil にするが
		// pushAnimTip は残る → その pull で履歴が書き換わり tip の SHA が消える →
		// 演出の着地でここへ来る。
		if m.hasCommitSHA(m.pushAnimTip) {
			m.awaitCI[m.pushAnimTip] = true
		}
		m.pushAnimTip = ""
	}
	all := make([]string, 0, len(m.commits))
	for _, c := range m.commits {
		if len(m.awaitCI) == 0 && m.statuses[c.SHA] == gitdata.StateUnpushed {
			m.awaitCI[c.SHA] = true // commits は新しい順なので最初の unpushed = tip
		}
		all = append(all, c.SHA)
		delete(m.statuses, c.SHA)
		delete(m.details, c.SHA)
	}
	m.invalidateLines()
	return m.startCIFetch(all)
}
