package main

import (
	"context"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/listnav"

	"subproc"

	"glogx/internal/commitview"
	"glogx/internal/extcmd"
	"glogx/internal/gitdata"
	"glogx/internal/term"

	"glogx/internal/app"

	tea "charm.land/bubbletea/v2"
)

// ciResultMsg は一括取得 1 チャンク分の結果。一括取得は chunkSHAs で複数チャンクへ割って
// 並列に投げるので、この msg は 1 回の取得につき複数回届く (startCIFetch 参照)。
// shas はそのチャンクが担当した SHA — unknown 埋め / detailsLoading 解除 / toFetch からの
// 差し引きをチャンクの範囲に閉じるために要る (全 toFetch を使うと、まだ飛んでいるチャンクの
// SHA を「応答に無かった」と誤判定して unknown に落としてしまう)。
type ciResultMsg struct {
	batch gitdata.CIBatch
	ghErr *gitdata.GHError
	shas  []string
	epoch int // 発行時点の fetchEpoch (世代不一致の遅延チャンクを捨てるため)
}

// detailMsg はパネル表示時のオンデマンド取得 (キャッシュヒットで詳細が無い SHA) の結果。
type detailMsg struct {
	sha   string
	batch gitdata.CIBatch
	ghErr *gitdata.GHError
}

// basisMsg は実行中 job の ETA 用に「同名完了 job の Duration」を補うための、表示中
// コミット (Details 未取得のもの) の一括取得結果。targets は取得を要求した SHA 群
// (レスポンスに現れなかったものも loading 解除する)。詳細は maybeFetchETABasis。
type basisMsg struct {
	targets []string
	batch   gitdata.CIBatch
	ghErr   *gitdata.GHError
}

// ciPollMsg は CI 追従ポーリングの周期タイマー。追従対象 (ciPollTargets) が 1 つでもある間、
// glogx を開いているあいだずっと回る — パネルの開閉・push からの経過時間・起動からの経過に
// 依存しない (「pending なら常に追う」の単一の出典)。gen は reloadAfterPull の世代で、
// リロード後に古いタイマーが二重チェーンとして復活するのを防ぐ。
type ciPollMsg struct{ gen int }

// ciPollResultMsg は ciPoll が投げた再取得の結果。一括取得 (ciResultMsg) と分けているのは、
// fetching / detailsLoading を立てず表示を「取得中」に落とさないため (チラつき防止)。
// gen は投げた時点の世代で、リロードを跨いで着弾した結果を捨てるために持つ
// (ciResultMsg の epoch と同じ役目)。
// 🚨 `targets` は持たない。一括取得 (`ciResultMsg`) は「レスポンスに現れなかった
// target も loading 解除する」ために要るが、こちらは **fetching / detailsLoading を立てない**
// ので解除する対象が無い。詰めても受け側が一度も読まない。
type ciPollResultMsg struct {
	gen   int
	batch gitdata.CIBatch
	ghErr *gitdata.GHError
}

const (
	ciPollInterval = 3 * time.Second
	// ciAwaitMaxAttempts は「push したのに CI が 1 つも現れない」ケースを諦めるまでの回数
	// (3s × 40 = 最長 2 分)。workflow を持たない repo で永久にポーリングしないための上限で、
	// pending が一度見えたら対象は awaitCI を卒業するのでこの上限は掛からない (完了まで追う)。
	ciAwaitMaxAttempts = 40
)

// rerunMsg は CI job 再実行要求 (r → y 確認後の gh run rerun --job) の結果。glogx の独自機能。
// sha は対象コミット (パネルリフレッシュの照合用)。
type rerunMsg struct {
	sha string
	err error
}

// rerunPollGrace は rerun 直後にパネル SHA を追従対象へ留める周期回数 (ciPollInterval × 10 = ~30s)。
// rerun を要求してから GraphQL に queued/in_progress が映るまでラグがあり、その間は状態が
// success/failure のままで追従対象にならない (パネルの ✗ が固まったままになる) ため、
// pending が見えるまでの間だけ空振りを許す。上限到達で諦める (反映は次の開き直しで)。
const rerunPollGrace = 10

// fetchCIStatusesCmd は targets の CI 状態取得を tea.Cmd にする。ctx/timeout/defer cancel の
// ボイラープレートを 1 箇所へ集約し、wrap で結果を各 msg (ciResult/detail/basis) に包む
// (レビュー U1)。同一 SHA 並行取得を避ける注意 (ciPollMsg / fetchPanelDetails のコメント)
// は呼び出し側のガードが担う。newBrowseModel の初期 fetch だけは m.cancel と ctx を共有して
// q 中断に使う意図的例外なので、この helper を通さず据え置く。
func fetchCIStatusesCmd(repo gitdata.Repo, targets []string, wrap func(gitdata.CIBatch, *gitdata.GHError) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), app.FetchTimeout)
		defer cancel()
		batch, ghErr := gitdata.FetchCIStatuses(ctx, gitdata.ExecRunner, repo, targets)
		return wrap(batch, ghErr)
	}
}

// startCIFetch は一括取得を chunkSHAs のチャンクへ割り、チャンクごとに ciResultMsg を返す Cmd を
// 束ねて返す。取得の「開始点」は 3 箇所 (起動 / reloadAfterPull / refetchAfterPush)
// あるので、状態 (toFetch / pendingFetches / ghErr) の立て方をここへ集約する。
//
// チャンク 1 が commits の表示順先頭なので、画面に映っているコミットの CI が最初に埋まる
// (ビューポート計算もカーソル追従も不要。commits が既に表示順なのを利用している)。
//
// ghErr はここでクリアする: チャンクごとに無条件代入すると、後から着弾した成功チャンクが先の
// 失敗チャンクの警告を消してしまう。「新しい取得で警告をリセットする」(レビュー C4 の
// sticky 警告防止) は開始時にやり、ハンドラ側は非 nil のときだけ立てる。
// fetching は一括取得のチャンクが 1 つでも未着か。**pendingFetches からの派生**であって
// 独立した状態ではない。フィールドとして並置すると、新しく「取得を始める /
// 終える」経路を足したときに代入を書き忘れ、スピナー (spinnerActive)・再描画ゲート
// (tickMsg)・パネルの「進行中の一括取得を待つ」ガード・ciPollMsg の並行抑止が
// **silent に狂う** (build もテストも通る)。導出にすればその失敗モードが構造的に消える。
func (m *browseModel) fetching() bool { return m.pendingFetches > 0 }

func (m *browseModel) startCIFetch(shas []string) tea.Cmd {
	shas, chunks := gitdata.PlanCIChunks(shas) // 超過分は StateUnknown のまま (newBrowseModel と同じポリシー)
	m.fetchEpoch++                             // 旧世代の未着チャンクを無効化 (ciResultMsg ハンドラの世代ガード)
	// 🚨 世代を進めると、旧世代のチャンクは ciResultMsg の epoch ガードで**丸ごと捨てられる**
	// = そこにあった「札を降ろす」処理も一緒に捨てられる。降ろす責任を負っていた札を、
	// 世代を進めるこの 1 箇所で解放する (gitlog_watch.go:handleGitLogProbe の
	// closed 経路が同じ規律を持っている)。
	//
	// 落とすのは**待機の札だけ**。実取得分岐の札は自前の detailMsg が降ろすので触らない。
	// 呼び出し側 (refetchAfterPush) に delete を足す形は採らない — 次に startCIFetch を
	// 呼ぶ経路が増えたときに同じ書き忘れが再発する。
	if len(m.detailsWaiting) > 0 {
		next := make(map[string]bool, len(shas))
		for _, sha := range shas {
			next[sha] = true
		}
		for sha := range m.detailsWaiting {
			if !next[sha] {
				m.clearDetailsFlags(sha)
			}
		}
	}
	m.toFetch = shas
	m.pendingFetches = len(chunks)
	m.ghErr = nil
	cmds := make([]tea.Cmd, 0, len(chunks))
	for _, chunk := range chunks {
		cmds = append(cmds, fetchCIChunkCmd(m.repo, chunk, m.fetchEpoch))
	}
	return tea.Batch(cmds...)
}

// fetchCIChunkCmd は 1 チャンク = GraphQL 1 リクエスト分の取得 Cmd。
//
// 🚨 FetchCIStatuses を通さないこと: あちらも内部で chunkSHAs するので、既に割ったチャンクを
// 渡すともう一段割られて同時リクエスト数が fetchConcurrency の二乗側 (最大 16 本) へ膨らむ。
// 分割はここ (startCIFetch) の 1 段だけに保つ。
func fetchCIChunkCmd(repo gitdata.Repo, chunk []string, epoch int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), app.FetchTimeout)
		defer cancel()
		batch, ghErr := gitdata.FetchCIChunk(ctx, gitdata.ExecRunner, repo, chunk)
		return ciResultMsg{batch: batch, ghErr: ghErr, shas: chunk, epoch: epoch}
	}
}

// mergeCIBatch は CIBatch 応答を browseModel のキャッシュへ吸収する。fetched (終了時 SaveCache
// 用の負キャッシュ) と statuses (表示用) は常に同一 source を受け、details / prCache も一緒に
// 更新される — この「4 キャッシュを 1 単位で co-update する」不変条件を ciResult/detail/basis の
// 3 ハンドラから 1 箇所へ局所化する (第 5 の co-update map が増えても touch は 1 箇所)。PR は
// コミット行のバッジ表示と p キーの両方で使う。site 固有の invalidateLines / ghErr クリア /
// detailsLoading 解除 / panelCursor クランプ / awaitCI 掃除は各ハンドラに残す (吸収の関心事ではない)。
func (m *browseModel) mergeCIBatch(statuses map[string]gitdata.CIState, details map[string][]gitdata.CheckDetail, prs map[string]*gitdata.PRRef) {
	maps.Copy(m.fetched, statuses)
	maps.Copy(m.statuses, statuses)
	maps.Copy(m.details, details)
	maps.Copy(m.prCache, prs)
}

func (m *browseModel) onCIResult(msg ciResultMsg) (tea.Model, tea.Cmd) {
	// 世代不一致 = pull/push の再取得 (startCIFetch) 前に飛ばした旧世代チャンク。
	// 丸ごと捨てる (データも捨てる: 同じ SHA は新世代のチャンクが取り直す)
	if msg.epoch != m.fetchEpoch {
		return m, m.maybeTick()
	}
	m.invalidateLines()
	// チャンクごとに届くので、成功チャンクで先の失敗チャンクの警告を消さない
	// (取得開始時のリセットは startCIFetch が担う)
	if msg.ghErr != nil {
		m.ghErr = msg.ghErr
	}
	// 応答に無かった SHA は unknown で埋める (fetched へ入れる = 終了時に SaveCache
	// される 30 秒の負キャッシュ)。q での中断 (fillUnknown) と違い、こちらは API の
	// 実際の返答に基づく確定。範囲はこのチャンクの SHA だけ — 全 toFetch で埋めると
	// まだ飛んでいるチャンクの SHA を「応答に無かった」と誤判定する
	filled := gitdata.FillUnknownFetched(msg.batch.Statuses, msg.shas)
	m.mergeCIBatch(filled, msg.batch.Details, msg.batch.PRs)
	// 未着チャンクが残っている間は fetching() が true のまま (スピナー・invalidate gate・
	// パネルガードの出典)。toFetch は着弾ぶんを差し引き、パネルが既に結果の来た SHA で
	// 待たされないようにする
	m.pendingFetches = max(m.pendingFetches-1, 0)
	// 🚨 ここを slices.DeleteFunc で書かないこと: あれは in-place 圧縮して破棄した末尾を
	// ゼロ埋めするが、chunkSHAs は元スライスの部分スライスを返すので m.toFetch と
	// 未着チャンクの msg.shas は同じ配列を共有している。in-place に削ると「まだ飛んでいる
	// チャンクの SHA 列」が空文字へ潰れ、その結果が届いても unknown 埋め・loading 解除の
	// 対象を失う。新しいスライスへ写して共有配列に触らない。
	remaining := make([]string, 0, len(m.toFetch))
	for _, sha := range m.toFetch {
		if !slices.Contains(msg.shas, sha) {
			remaining = append(remaining, sha)
		}
	}
	m.toFetch = remaining
	// 一括取得待ちでパネルを開いていた SHA の loading を解除する (結果が来なかった
	// SHA も含めて解除。details 不在は「(CI job 情報なし)」表示に落ちる)
	for _, sha := range msg.shas {
		m.clearDetailsFlags(sha)
	}
	m.settleAwaitCI()
	// 一括取得で実行中コミットの Details が入った場合も ETA basis を補充する
	// (パネルを取得中に開いていたケース)。pending が見えたらここで追従チェーンが張られる
	// (起動直後に既に pending なコミットがある場合の開始点でもある)。
	return m, tea.Batch(m.maybeFetchETABasis(), m.ensureCIPoll())
}

func (m *browseModel) onDetail(msg detailMsg) (tea.Model, tea.Cmd) {
	m.invalidateLines()
	m.clearDetailsFlags(msg.sha)
	m.ghErr = msg.ghErr // 成功時 (nil) はクリア: ciResultMsg と揃える (sticky 警告の防止・レビュー C4)
	m.mergeCIBatch(msg.batch.Statuses, msg.batch.Details, msg.batch.PRs)
	// リフレッシュで job 数が縮んだ場合にフォーカスを範囲内へ戻す
	if msg.sha == m.panelSHA && m.panelCursor >= len(m.details[m.panelSHA]) {
		m.panelCursor = len(m.details[m.panelSHA]) - 1
	}
	// ETA basis (同名完了 job) の補充と追従チェーンの開始をここで行う (openPanel 時点では
	// details 未取得で pending かどうか判定できない)。ensureCIPoll は single-flight なので
	// 既にチェーンが生きていれば nil を返す (二重 timer で加速しない)。
	return m, tea.Batch(m.maybeFetchETABasis(), m.ensureCIPoll())
}

func (m *browseModel) onBasis(msg basisMsg) (tea.Model, tea.Cmd) {
	m.invalidateLines()
	m.ghErr = msg.ghErr // 成功時 (nil) はクリア: ciResultMsg と揃える (レビュー C4)
	m.mergeCIBatch(msg.batch.Statuses, msg.batch.Details, msg.batch.PRs)
	// レスポンスに現れなかった target も含めて loading 解除し、Details エントリを
	// 空スライスで確定させる (未設定のままだと同じ target を無限に取り直してしまう)。
	for _, sha := range msg.targets {
		m.clearDetailsFlags(sha)
		if _, ok := m.details[sha]; !ok {
			m.details[sha] = []gitdata.CheckDetail{}
		}
	}
	return m, nil
}

func (m *browseModel) onJobDetail(msg commitview.JobDetailMsg) (tea.Model, tea.Cmd) {
	m.ghErr = msg.Err() // 成功時 (nil) はクリア: ciResultMsg と揃える (レビュー C4)。
	// 🚨 ghErr は共有 sticky 警告なので detailOv.receive に閉じず browseModel で無条件代入する。
	// receive は busy 落とし・cache 格納・(今開いている詳細なら) 末尾スクロールを担う。currentKey は
	// live な detailKey() を渡す (snapshot 禁止: リフレッシュで panelCursor がクランプされ得るため)。
	m.detailOv.Receive(msg, m.detailKey(), m.visibleDetailRows())
	// Y で詳細取得を待っていたら、到着したこの内容をコピーする。取得失敗
	// (ghErr) は上の sticky 警告に任せ、予約だけ静かに破棄する。
	// 🚨 予約後にフォーカスが動いていたら (detailKey() != msg.key) コピーしない: msg.lines は
	// 予約時の job のログだが、focusedJob() は現フォーカスを返すため、貼ると「別 job のヘッダに
	// 旧 job の本文」という silent 誤コピーになる。詳細ポップアップを閉じただけ (closePanel を
	// 経ない) でカーソル移動できる経路があり、copyOnDetail が残るため起きる (レビュー確定 high)。
	if m.copyOnDetail == msg.Key() {
		m.copyOnDetail = ""
		if job, ok := m.focusedJob(); ok && m.detailKey() == msg.Key() && msg.Err() == nil && len(msg.Lines()) > 0 {
			m.copyJobContextLines(job, msg.Lines())
		}
	}
	return m, nil
}

func (m *browseModel) onCIPoll(msg ciPollMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.ciPollGen {
		return m, nil // reloadAfterPull で世代が進んだ後の残タイマーは破棄
	}
	targets := m.ciPollTargets()
	if len(targets) == 0 {
		// 🚨 **ここで awaitCI を空にする**。ciPollTargets は commits を走査して
		// `m.awaitCI[c.SHA]` を targets に入れるので、**targets が空なら残っている awaitCI の
		// 要素は定義上 commits の外**にいる = どの経路でも取り除かれないファントム。
		// ⚠️ 打ち切り (awaitAttempts) を早期 return の前へ動かしても効かない: この分岐は
		// ciPolling=false でチェーンごと止めるため、1 回数えても次の周期が来ない。
		m.awaitCI, m.awaitAttempts = nil, 0
		m.ciPolling = false // 追従対象なし: チェーンを止める (次の開始点で再アーム)
		return m, nil
	}
	if m.panelGrace > 0 {
		m.panelGrace-- // rerun 直後: 新しい実行が GraphQL に映るまで空振りを許す
	}
	// 「CI が 1 つも現れない」の打ち切りは周期の側で数える。結果着弾の側で数えると、
	// 一括取得が複数チャンクへ割れた回に awaitAttempts が余分に進み、意図した
	// ciPollInterval × ciAwaitMaxAttempts (= 2 分) より早く諦めてしまう
	if len(m.awaitCI) > 0 {
		m.awaitAttempts++
		if m.awaitAttempts >= ciAwaitMaxAttempts {
			m.awaitCI = nil // 諦める: workflow を持たない repo で永久にポーリングしない
		}
	}
	next := m.scheduleCIPoll()
	// 別経路の取得と重ねない (同一 SHA への GraphQL 並行は完了順で statuses/details が
	// 上書きされる。fetchPanelDetails と同じ注意)。タイマーだけ繋いで次の周期に回す
	if m.ciPollInFlight || m.fetching() {
		return m, next
	}
	m.ciPollInFlight = true
	gen := m.ciPollGen
	fetch := ciPollFetch(m.repo, targets, func(b gitdata.CIBatch, e *gitdata.GHError) tea.Msg {
		return ciPollResultMsg{gen: gen, batch: b, ghErr: e}
	})
	return m, tea.Batch(fetch, next, m.maybeTick())
}

func (m *browseModel) onCIPollResult(msg ciPollResultMsg) (tea.Model, tea.Cmd) {
	// 🚨 in-flight は世代不一致でも必ず下ろす: リロード側は「飛んでいる poll の結果が
	// 着弾するまで in-flight を維持する」前提 (reloadAfterPull のコメント) なので、
	// ここで下ろさないと以降の周期が永久に fetch を見送る
	m.ciPollInFlight = false
	if msg.gen != m.ciPollGen {
		// リロード前に投げた結果。マージすると入れ替わった statuses を古い観測で
		// 巻き戻す (決着済みの SHA が pending に戻り、表示と追従が 1 周期ぶれる)
		return m, m.ensureCIPoll()
	}
	m.invalidateLines()
	m.ghErr = msg.ghErr // 成功時 (nil) はクリア: ciResultMsg と揃える (sticky 警告の防止)
	m.mergeCIBatch(msg.batch.Statuses, msg.batch.Details, msg.batch.PRs)
	m.settleAwaitCI()
	// リフレッシュで job 数が縮んだ場合にフォーカスを範囲内へ戻す
	if m.panelSHA != "" && m.panelCursor >= len(m.details[m.panelSHA]) {
		m.panelCursor = max(len(m.details[m.panelSHA])-1, -1)
	}
	return m, tea.Batch(m.maybeFetchETABasis(), m.ensureCIPoll())
}

func (m *browseModel) onRerun(msg rerunMsg) (tea.Model, tea.Cmd) {
	m.actModal.rerunning = false
	if msg.err != nil {
		m.showWarning("再実行に失敗: " + app.FirstLine(msg.err.Error()))
		return m, m.maybeTick()
	}
	m.toast.Show("CI を再実行します", true)
	// パネルを開いたままなら猶予つきで追従対象に留める (rerun が GraphQL に映るまでのラグは
	// rerunPollGrace のコメント参照)。映れば StatePending になり、以降は通常の追従へ
	// 自然に引き継がれる。パネルが閉じられていれば何もしない (次の開き直しで最新を取る)
	var poll tea.Cmd
	if msg.sha == m.panelSHA && m.panelSHA != "" {
		m.panelGrace = rerunPollGrace
		poll = m.ensureCIPoll() // 既にチェーンが生きていれば nil (二重化しない)
	}
	return m, tea.Batch(poll, m.maybeTick())
}

// handlePanelKey は job パネル表示中のキー操作。j/k はパネル内のフォーカス移動になる。
// Enter は一貫して「TUI 内で開閉 (toggle)」: タイトル行 = パネルを閉じる (Enter 連打で
// 開閉 toggle)、job 行 = 詳細 (annotations / ログ tail) ポップアップを開く。
// ブラウザで開くのは o (ユーザー要望)。
func (m *browseModel) handlePanelKey(key string) (tea.Model, tea.Cmd) {
	if m.detailOv.Visible() {
		return m.handleDetailKey(key)
	}
	jobs := m.details[m.panelSHA]
	switch key {
	case "esc", "h", "left":
		m.closePanel()
	case "enter", " ":
		if m.panelCursor < 0 {
			m.closePanel()
			return m, nil
		}
		return m, m.openJobDetail()
	case "l", "right", "tab":
		if m.panelCursor < 0 {
			if len(jobs) > 0 {
				m.panelCursor = 0
			}
			return m, nil
		}
		return m, m.openJobDetail()
	case "o":
		return m, m.openJob()
	case "y":
		m.copyFocusURL()
	case "Y":
		return m, m.copyJobContext()
	case "p":
		return m, m.openPR()
	case "P":
		// パネル内では PR 状態ポップアップを開かない仕様 (重ね順が複雑化する)。他の無効操作
		// (job 未選択の o/r 等) と同様、無反応でなく理由をトーストで返す
		m.toast.Show("PR 状態はパネルを閉じてから P で表示します", false)
	case "d":
		return m, m.openDiff()
	case "r":
		m.askRerun()
	default:
		m.applyPanelMotion(listnav.MotionOf(key), len(jobs)) // 移動の語彙は listnav が持つ
	}
	return m, nil
}

// applyPanelMotion は job パネルのカーソル移動を適用する。カーソルは -1 (タイトル行) から
// jobs-1 まで。半ページは持たない (job は数件で画面に収まり、送る先が無い)。
func (m *browseModel) applyPanelMotion(mo listnav.Motion, jobs int) {
	switch mo {
	case listnav.Down:
		if m.panelCursor+1 < jobs {
			m.panelCursor++
		}
	case listnav.Up:
		m.panelCursor = max(m.panelCursor-1, -1)
	case listnav.Top:
		// job 0 件でフォーカスを 0 にすると、存在しない job にフォーカスが移って
		// タイトル行 (-1) へ戻れなくなる (Enter で閉じられない) ため空ではしない
		if jobs > 0 {
			m.panelCursor = 0
		}
	case listnav.Bottom:
		if jobs > 0 {
			m.panelCursor = jobs - 1
		}
	case listnav.HalfDown, listnav.HalfUp, listnav.None:
	}
}

// handleDetailKey は job 詳細ポップアップ表示中のキー操作。o(ブラウザ)/v(nvim)/y(コピー) の
// 越境キーはここで処理し、スクロール/閉じ (enter/space/esc/h/left/j/k/g/G/pg) は detailOv.scroll へ
// 委譲する (handleDiffKey が y を残して scroll を委譲するのと同型)。cache キー/表示行数はレイアウト・
// パネル状態依存なので detailKey()/visibleDetailRows() を引数で注入する。
func (m *browseModel) handleDetailKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "o":
		return m, m.openJob()
	case "v":
		return m, m.openJobLogInEditor()
	case "y":
		m.copyFocusURL()
		return m, nil
	case "Y":
		return m, m.copyJobContext()
	case "r":
		m.askRerun()
		return m, nil
	case "J", "shift+down":
		return m, m.openNeighborJobDetail(1)
	case "K", "shift+up":
		return m, m.openNeighborJobDetail(-1)
	}
	m.detailOv.Scroll(key, m.detailKey(), m.visibleDetailRows())
	return m, nil
}

// openNeighborJobDetail は job 詳細を開いたまま隣 (delta = ±1) の job へ差し替える
// (J/K。docs/glogx-ui-guide.md §6)。パネルのフォーカスも追従させ、端では止めて案内する。
// 開き直しは openJobDetail に任せる (cache ヒットなら末尾へ、未取得なら取得を発行する)。
func (m *browseModel) openNeighborJobDetail(delta int) tea.Cmd {
	jobs := m.details[m.panelSHA]
	ni := m.panelCursor + delta
	if ni < 0 || ni >= len(jobs) {
		if delta > 0 {
			m.toast.Show("これが最後の job です", false)
		} else {
			m.toast.Show("これが最初の job です", false)
		}
		return nil
	}
	m.panelCursor = ni
	return m.openJobDetail()
}

// copyJobContext はフォーカス中 job の「何が起きたか」(step 一覧 + annotations / ログ末尾) を
// Markdown 整形でクリップボードへコピーする (job パネル / job 詳細の Y キー。LLM に貼る用)。詳細が未取得なら詳細ポップアップを開いて取得し、到着 (jobDetailMsg) 時にコピーする。
func (m *browseModel) copyJobContext() tea.Cmd {
	job, ok := m.focusedJob()
	if !ok {
		m.toast.Show("job を選択してから Y で詳細をコピーします", false)
		return nil
	}
	if lines := m.detailOv.Lines(m.detailKey()); len(lines) > 0 {
		m.copyJobContextLines(job, lines)
		return nil
	}
	m.copyOnDetail = m.detailKey()
	return m.openJobDetail()
}

// copyJobContextLines は job 詳細行をヘッダ (job 名 / commit / URL) 付きの Markdown にして
// クリップボードへ入れる。ヘッダ・本文とも制御コードを除去したプレーンテキストにする。
// job.URL (= StatusContext の targetUrl 等) は外部 CI が任意に設定できる。detailsOf が受け取った時点で
// 平文にしているが、クリップボードは描画処理のような後ろ盾の無い最後の関門なので、ここでも通す
// (受け取る側の 1 箇所だけに頼ると、CheckDetail を別の経路で組んだときに素通りする)。
// 🚨 term.StripANSI 単体は OSC を落とせない (英字終端判定のため OSC の途中で誤終了し BEL が残る・実測) ので、
// term.SanitizeDetailLine を先に通す順を崩さないこと。
// c.Subject は %q が制御文字を Go エスケープするため安全。本文 lines は取得時に
// term.SanitizeDetailLine 済みなので jobLogText の term.StripANSI だけで足りる。
func (m *browseModel) copyJobContextLines(job gitdata.CheckDetail, lines []string) {
	plain := func(s string) string { return term.StripANSI(term.SanitizeDetailLine(s)) }
	var b strings.Builder
	b.WriteString("## CI job: ")
	b.WriteString(plain(job.Name))
	if c := m.commitBySHA(m.panelSHA); c != nil && m.hasRepo {
		fmt.Fprintf(&b, " — %s/%s@%s %q", m.repo.Owner, m.repo.Name, c.ShortSHA, c.Subject)
	}
	b.WriteString("\n")
	if job.URL != "" {
		b.WriteString(plain(job.URL))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(jobLogText(lines))
	m.copyWithToast(b.String(), fmt.Sprintf("job 詳細をコピーしました (%d 行)", len(lines)))
}

// askRerun はフォーカス中 job の CI 再実行確認 (y/N) に入る (job パネル / job 詳細の r キー)。
// 再実行できないケース (StatusContext / 失敗以外) は確認を出さず notice で理由を伝える。
func (m *browseModel) askRerun() {
	job, ok := m.focusedJob()
	if !ok {
		// タイトル行フォーカス (job 未選択) で r。o/Y と揃えて選択を促す。
		m.toast.Show("job を選択してから r で再実行します", false)
		return
	}
	if job.CheckID == 0 {
		m.toast.Show("GitHub Actions の job ではないため再実行できません", false)
		return
	}
	if job.State != gitdata.StateFailure {
		m.toast.Show("再実行できるのは失敗した job だけです", false)
		return
	}
	repo, sha, id := m.repo, m.panelSHA, job.CheckID
	m.actModal.askRerun(job.Name, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), app.FetchTimeout)
		defer cancel()
		return rerunMsg{sha: sha, err: runJobRerun(ctx, repo, id)}
	})
}

// detailKey は job 詳細キャッシュのキー。詳細表示中は panelCursor が動かないため安定する。
func (m *browseModel) detailKey() string {
	return fmt.Sprintf("%s/%d", m.panelSHA, m.panelCursor)
}

// focusedJob はパネルでフォーカス中の job を返す (タイトル行フォーカス・範囲外・
// パネル非表示は ok=false)。境界条件の実装をここ 1 箇所に集約する。
func (m *browseModel) focusedJob() (gitdata.CheckDetail, bool) {
	jobs := m.details[m.panelSHA]
	if m.panelCursor < 0 || m.panelCursor >= len(jobs) {
		return gitdata.CheckDetail{}, false
	}
	return jobs[m.panelCursor], true
}

// etaBasis は実行中 job (name) の終了予定を概算するための「直近の同名完了 job 1 件」の
// 所要時間を返す。excludeSHA (実行中の当該コミット) を除き、表示中コミットを
// excludeSHA に近い順 (まず古い側、次に新しい側) に走査して最初に見つかった同名 job を
// 採用する。追加 fetch はしない (画面に取得済みの Details だけで概算する)。
// 見つからなければ ok=false (履歴が画面に無い / 初回実行)。
//
// StateNeutral (cancelled / skipped) は除外する: 途中 cancel された run も StartedAt/
// CompletedAt を持ち Duration>0 になるが、数秒で切られた時間を basis にすると ETA が
// 極端に短く出て即「予定超過」になり概算が誤る。完了まで走った失敗 (StateFailure) は
// 所要時間として妥当なので basis に残す。
func (m *browseModel) etaBasis(name, excludeSHA string) (time.Duration, bool) {
	idx := -1
	for i := range m.commits {
		if m.commits[i].SHA == excludeSHA {
			idx = i
			break
		}
	}
	if idx < 0 {
		return 0, false
	}
	for dist := 1; dist < len(m.commits); dist++ {
		for _, j := range [2]int{idx + dist, idx - dist} { // 古い側 (下) を先に見る
			if j < 0 || j >= len(m.commits) {
				continue
			}
			for _, det := range m.details[m.commits[j].SHA] {
				if det.Name == name && det.Duration > 0 && det.State != gitdata.StateNeutral {
					return det.Duration, true
				}
			}
		}
	}
	return 0, false
}

// jobTimeSuffix は job 行末尾の時間表示 ("(...)" の中身。空 = 何も出さない)。
//   - 実行中 (StatePending) で開始時刻あり: "2m10s 経過" + ETA basis があれば "残り ~50s" / "予定超過"
//   - 完了: 所要時間 (従来どおり)
//
// 実行中判定は State で行う (Duration==0 は StatusContext / 未取得も含むため出典にしない)。
func (m *browseModel) jobTimeSuffix(job gitdata.CheckDetail) string {
	if job.Running() {
		elapsed := app.TimeNow().Sub(job.StartedAt)
		el := gitdata.FormatDuration(elapsed)
		if el == "" { // 開始直後 (<1s) / わずかな時計ずれ
			el = "0s"
		}
		suffix := el + " 経過"
		if basis, ok := m.etaBasis(job.Name, m.panelSHA); ok {
			if remain := basis - elapsed; remain > 0 {
				suffix += ", 残り ~" + gitdata.FormatDuration(remain)
			} else {
				suffix += ", 予定超過"
			}
		}
		return suffix
	}
	return gitdata.FormatDuration(job.Duration)
}

// jobDetailRows は詳細ポップアップに一度に表示する行数の上限。実際の行数は
// 端末の高さに合わせて visibleDetailRows が縮める。
const jobDetailRows = 15

// visibleDetailRows は詳細ポップアップが実際に使える行数 (job パネルとヒント行を
// 差し引いた残り。低い端末で詳細ボックスがビューポートに切られ、末尾スクロールが
// 見えなくなるのを防ぐ)。-4 = 詳細の枠 2 行 + パネル・詳細それぞれの下端落ち影 1 行ずつ。
func (m *browseModel) visibleDetailRows() int {
	jobBoxLines := min(max(len(m.details[m.panelSHA]), 1), maxPanelJobs) + 2
	return max(min(jobDetailRows, m.pageSize()-jobBoxLines-4), 3)
}

// openJobDetail はフォーカス中 job の annotations / ログ tail のポップアップを開く。
func (m *browseModel) openJobDetail() tea.Cmd {
	check, ok := m.focusedJob()
	if !ok {
		return nil
	}
	key := m.detailKey()
	if !m.detailOv.StartOpen(key, m.visibleDetailRows()) {
		return nil // cache ヒット (offset は末尾へ) / 取得中: 追加取得は不要
	}
	repo := m.repo
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), app.FetchTimeout)
		defer cancel()
		lines, ghErr := gitdata.FetchJobDetail(ctx, gitdata.ExecRunner, repo, check)
		return commitview.NewJobDetailMsg(key, lines, ghErr)
	}
	return tea.Batch(cmd, m.maybeTick())
}

// openPanel はカーソル位置のコミットの CI job パネルを開く。詳細が未取得
// (キャッシュヒットで一括取得に含まれなかった SHA) なら、その SHA だけ追加取得する。
func (m *browseModel) openPanel() tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	sha := m.commits[m.cursor].SHA
	m.panelSHA = sha
	m.panelCursor = -1 // タイトル行フォーカスから開始 (この状態の Enter = 閉じる)
	// パネルコミットの Details 取得と ETA basis の補充を仕掛ける。details 既取得なら basis 補充
	// だけ即走る (basis 判定に details が要るため)。追従チェーンはパネルとは独立して pending で
	// 回っているので、ここでは single-flight の ensureCIPoll を通すだけ (未取得なら detailMsg 側)。
	return tea.Batch(m.fetchPanelDetails(sha), m.maybeFetchETABasis(), m.ensureCIPoll())
}

// ciPollTargets は「まだ決着していないので取り直す」SHA。追従の条件はこの 1 箇所だけが決める:
//
//   - StatePending のコミット: 完了 (success/failure/neutral) まで無条件に追う。パネルを開いて
//     いるかどうか・push からどれだけ経ったかは問わない
//   - 取得済み Details に実行中 job がある SHA: ロールアップが pending にならない組み合わせ
//     (「1 件失敗 + 1 件実行中」は aggregateRollup が失敗優先で StateFailure) を拾うため。
//     これが無いと、失敗を含むコミットの残り job や rerun した job の経過時間が固まる
//   - awaitCI: push 直後で CI がまだ 1 つも見えない SHA (ciAwaitMaxAttempts で打ち切る)
//   - panelGrace 中のパネル SHA: rerun 要求が GraphQL に映るまでのラグ吸収
func (m *browseModel) ciPollTargets() []string {
	seen := make(map[string]bool, len(m.commits))
	targets := make([]string, 0, len(m.commits))
	add := func(sha string) {
		if sha == "" || seen[sha] {
			return
		}
		seen[sha] = true
		targets = append(targets, sha)
	}
	for _, c := range m.commits {
		if m.statuses[c.SHA] == gitdata.StatePending || m.awaitCI[c.SHA] || m.hasRunningJob(c.SHA) {
			add(c.SHA)
		}
	}
	if m.panelGrace > 0 {
		add(m.panelSHA)
	}
	return gitdata.CapFetchSHAs(targets)
}

// ciPollFetch は ciPoll の再取得を張る差し替え点 (本体は fetchCIStatusesCmd)。並行取得を
// 弾くガードをテストが回数で観測するために変数にしている (runJobRerun と同じ理由)。
var ciPollFetch = fetchCIStatusesCmd

// scheduleCIPoll は現世代の ciPollMsg を ciPollInterval 後に発火させる (チェーンの「継続」用)。
// チェーンの「開始」は必ず ensureCIPoll を通す。
func (m *browseModel) scheduleCIPoll() tea.Cmd {
	gen := m.ciPollGen
	return tea.Tick(ciPollInterval, func(time.Time) tea.Msg { return ciPollMsg{gen: gen} })
}

// ensureCIPoll は ciPollMsg の自己更新チェーンを single-flight で 1 本だけ張る (maybeTick と
// 同型)。既に生きている / 追従対象が無いときは nil を返す。開始点が複数あっても
// (起動時の ciResultMsg / detailMsg / refetchAfterPush / rerunMsg / openPanel) チェーンが
// 二重化してポーリング頻度が倍にならないのはこのガードによる。
func (m *browseModel) ensureCIPoll() tea.Cmd {
	if m.ciPolling || len(m.ciPollTargets()) == 0 {
		return nil
	}
	m.ciPolling = true
	return m.scheduleCIPoll()
}

// settleAwaitCI は「push したが CI がまだ見えない」SHA の後始末。CI が見えたら awaitCI を
// 卒業させ、見えていない SHA は結果を捨てる (statuses から消してスピナーに戻し、fetched からも
// 外してファイルキャッシュへ「checks なし」を負キャッシュとして残さない)。上限に達したら諦める。
// hasCommitSHA は sha が今の commits に在るか (awaitCI の不変条件の判定に使う)。
func (m *browseModel) hasCommitSHA(sha string) bool {
	return slices.ContainsFunc(m.commits, func(c gitdata.Commit) bool { return c.SHA == sha })
}

func (m *browseModel) settleAwaitCI() {
	if len(m.awaitCI) == 0 {
		return
	}
	for sha := range m.awaitCI {
		// 🚨 **commits に無い SHA は毎周期ここで落とす**。入口 (refetchAfterPush)
		// でも弾いているが、awaitCI を張る経路が増えたときにこちらが最後の砦になる。
		// 落とさないと statuses が永久に現れず、下の switch は default 側を回り続けるだけで
		// awaitCI から消えない = スピナーと再描画が止まらない。
		if !m.hasCommitSHA(sha) {
			delete(m.awaitCI, sha)
			continue
		}
		switch m.statuses[sha] {
		case gitdata.StatePending, gitdata.StateSuccess, gitdata.StateFailure, gitdata.StateNeutral:
			delete(m.awaitCI, sha) // CI が見えた: 以降は statuses 起点の通常の追従へ
		default:
			delete(m.statuses, sha)
			delete(m.fetched, sha)
		}
	}
	m.invalidateLines()
}

// fetchPanelDetails はパネルコミット sha の Details をオンデマンド取得する Cmd
// (取得不要 / 取得先なしは nil)。openPanel から切り出した本体。
// clearDetailsFlags は sha の「取得中」の札を降ろす。
//
// 🚨 **detailsLoading と detailsWaiting は必ず対で消すこと。** detailsWaiting は
// detailsLoading の部分集合 (上の宣言) なので、片方だけ消すと
// 「waiting=true / loading=false」の孤児が残る。孤児が残ると fetchPanelDetails の
// 早期 return が外れて実取得が飛び、その札を startCIFetch の世代交代が
// **飛んでいる GraphQL の札として落とす** — 272 のコメントが名指しで禁じている形になる
// (実測 2026-09-06: detailMsg / basisMsg が loading しか消していなかった)。
func (m *browseModel) clearDetailsFlags(sha string) {
	delete(m.detailsLoading, sha)
	delete(m.detailsWaiting, sha)
}

func (m *browseModel) fetchPanelDetails(sha string) tea.Cmd {
	if _, ok := m.details[sha]; ok || m.detailsLoading[sha] {
		return nil
	}
	if !m.hasRepo || m.statuses[sha] == gitdata.StateUnpushed {
		// remote が GitHub でない / 未 push の SHA は取得先が無い
		m.details[sha] = []gitdata.CheckDetail{}
		return nil
	}
	// 進行中の一括取得に含まれる SHA は、その結果 (details 込み) を待つ。
	// ここで別リクエストを打つと同一 SHA への GraphQL が並行し、完了順で
	// statuses/details が上書きされる
	if m.fetching() && slices.Contains(m.toFetch, sha) {
		// Cmd を 1 本も出さない = 降ろす責任は一括取得側にある。世代交代で捨てられたときに
		// 取り残されないよう、待機であることを覚えておく。
		m.detailsLoading[sha], m.detailsWaiting[sha] = true, true
		return nil
	}
	m.detailsLoading[sha] = true
	cmd := fetchCIStatusesCmd(m.repo, []string{sha}, func(b gitdata.CIBatch, e *gitdata.GHError) tea.Msg {
		return detailMsg{sha: sha, batch: b, ghErr: e}
	})
	return tea.Batch(cmd, m.maybeTick())
}

// maybeFetchETABasis は、パネルを開いた実行中コミットに ETA basis (同名完了 job の
// Duration) がセッション内に無いとき、表示中の他コミットのうち Details 未取得のものを
// 1 回の GraphQL でまとめて取得する Cmd を返す (nil = 取得不要 / 取得先なし)。
//
// 完了コミットは State だけがキャッシュされ Details は保存されないため (cache.go)、glogx を
// 開き直すと完了状態は cache ヒットで toFetch から外れ、その job Duration が欠けて ETA が
// 出なくなる。パネルを開いた時点でこの穴を能動的に埋める。取得は 1 リクエストに束ね、
// 対象は表示中コミットに限る (無制限に履歴を遡らない)。
func (m *browseModel) maybeFetchETABasis() tea.Cmd {
	if m.panelSHA == "" || !m.hasRepo {
		return nil
	}
	jobs, ok := m.details[m.panelSHA]
	if !ok {
		return nil // パネルコミットの Details 未取得。到着後 (detailMsg) に再評価される
	}
	// basis を必要とする実行中 job があり、かつ現状 basis が取れないときだけ補充する
	need := false
	for _, j := range jobs {
		if j.Running() {
			if _, ok := m.etaBasis(j.Name, m.panelSHA); !ok {
				need = true
				break
			}
		}
	}
	if !need {
		return nil
	}
	var targets []string
	for _, c := range m.commits {
		switch {
		case c.SHA == m.panelSHA:
		case m.detailsLoading[c.SHA]:
		case m.statuses[c.SHA] == gitdata.StateUnpushed:
		case m.fetching() && slices.Contains(m.toFetch, c.SHA): // 進行中の一括取得を待つ
		default:
			if _, ok := m.details[c.SHA]; !ok { // Details 未取得のものだけ
				targets = append(targets, c.SHA)
			}
		}
		if len(targets) >= gitdata.FetchMaxSHAs {
			break
		}
	}
	if len(targets) == 0 {
		return nil
	}
	for _, sha := range targets {
		m.detailsLoading[sha] = true
	}
	cmd := fetchCIStatusesCmd(m.repo, targets, func(b gitdata.CIBatch, e *gitdata.GHError) tea.Msg {
		return basisMsg{targets: targets, batch: b, ghErr: e}
	})
	return tea.Batch(cmd, m.maybeTick())
}

func (m *browseModel) closePanel() {
	m.panelSHA = ""
	m.panelCursor = -1
	// パネルを閉じても CI 追従ポーリングは止めない (pending なら追い続けるのが不変条件)。
	// 閉じるのはパネル固有の状態だけ。
	m.panelGrace = 0    // rerun 直後の猶予はパネルと一緒に終える (パネル SHA を狙う猶予なので)
	m.copyOnDetail = "" // Y のコピー予約も破棄 (閉じた後の到着で意図しないコピーをしない)
	m.detailOv.Close()  // 詳細ポップアップも閉じる (panel/detail 両クラスタの choke point)
}

// jobLogText は job 詳細行を nvim へ渡すプレーンテキストにする。ANSI 色 (SGR) を除去して、
// nvim で yank したときに制御コードが混ざらないようにする。
func jobLogText(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(term.StripANSI(l))
		b.WriteByte('\n')
	}
	return b.String()
}

// openJobLogInEditor は表示中の job 詳細ログを nvim で開く (v キー・コピー用。less の v=エディタ
// で開く慣習に合わせた「view」)。ログは stdin
// (`nvim -`) で渡すのでディスクにファイルを残さず、nvim を閉じればバッファごと破棄される
// (ユーザー要望 2026-07-21: ログのテキストをコピーしたいが後に残したくない)。
func (m *browseModel) openJobLogInEditor() tea.Cmd {
	lines := m.detailOv.Lines(m.detailKey())
	if len(lines) == 0 {
		m.toast.Show("開けるログがありません", false)
		return nil
	}
	// -R (readonly) + nomodifiable + buftype=nofile で「閲覧してコピーするだけ」の scratch に
	// する: 誤編集できず :q が常にクリーンに閉じる (素の nvim - だと変更扱い等で :q がエラーに
	// なる・ユーザー報告 2026-07-21)。yank は nomodifiable でも可能。noswapfile で swap も残さない。
	//
	// 🚨 ここは $EDITOR を見ずに nvim 固定にする: 開くのが実ファイルでなく標準入力 (`-`) で、
	// scratch 化も vim 系の -c/-R に依存しているため、任意のエディタでは成立しない
	// (code - / nano - は不可)。実ファイルを開く経路の $EDITOR 対応は extcmd.EditorCommand の doc を参照。
	cmd := exec.Command("nvim", "-R", "-c", "setlocal buftype=nofile noswapfile nomodifiable", "-")
	cmd.Stdin = strings.NewReader(jobLogText(lines))
	// 🚨 ここは注記で免除しない。Stdin が *os.File でない (strings.Reader) ため os/exec は
	// os.Pipe と copy goroutine を作る (GOROOT os/exec/exec.go childStdin)。つまり他の前景実行と
	// 違い「パイプが無いから安全」が成り立たず、nvim の孫が読み口を握ったまま nvim だけ終わると
	// Wait が戻らない。ctx が無く kill する主体もいないので、上限は WaitDelay しかない。
	cmd.WaitDelay = subproc.WaitDelay
	return extcmd.RunEditorCmd(extcmd.OriginEditor, cmd)
}

// openJob はパネルで選択中の job の詳細ページをブラウザで開く。
func (m *browseModel) openJob() tea.Cmd {
	job, ok := m.focusedJob()
	if !ok {
		// タイトル行フォーカス (job 未選択) で o。Y (copyJobContext) と揃えて選択を促す
		// (job パネルの o/r/Y は job 未選択なら無反応なので理由をトーストで出す)。
		m.toast.Show("job を選択してから o で開きます", false)
		return nil
	}
	if job.URL == "" {
		m.toast.Show("この job には詳細ページの URL がありません", false)
		return nil
	}
	return m.openURLCmd(job.URL)
}

// listHasPending は一覧に CI 実行中のコミットがあるか (その header 行はスピナーを描く)。
func (m *browseModel) listHasPending() bool {
	for _, c := range m.commits {
		if m.statuses[c.SHA] == gitdata.StatePending {
			return true
		}
	}
	return false
}

// panelHasPendingJob はパネルに queued / 実行中の job があるか。tick を回し続けて、job 行の
// スピナーと実行中の job の「N 経過 / 残り ~M」をライブ更新する (spinnerActive が false だと
// tick が止まり、パネルを開いたままスピナーも経過秒も固まる)。
func (m *browseModel) panelHasPendingJob() bool {
	if m.panelSHA == "" {
		return false
	}
	for _, job := range m.details[m.panelSHA] {
		if job.State == gitdata.StatePending {
			return true
		}
	}
	return false
}

// hasRunningJob は sha の取得済み Details に実行中 job があるか。
// 🚨 statuses (ロールアップ) では代用できない: aggregateRollup は失敗を最優先するので
// 「1 件失敗 + 1 件実行中」は StateFailure になり、pending 判定では実行中を取りこぼす。
func (m *browseModel) hasRunningJob(sha string) bool {
	if sha == "" {
		return false
	}
	for _, job := range m.details[sha] {
		if job.Running() {
			return true
		}
	}
	return false
}

// panelLines は job パネルの描画行 (枠付き)。パネル非表示なら nil。
func (m *browseModel) panelLines() []string {
	if m.panelSHA == "" {
		return nil
	}
	width := m.contentWidth()
	if width <= 0 {
		width = 80
	}
	commit := m.commitBySHA(m.panelSHA)
	if commit == nil {
		return nil
	}
	jobs, haveDetails := m.details[m.panelSHA]
	var rows []string
	switch {
	case m.detailsLoading[m.panelSHA]:
		rows = []string{term.Paint(m.spinner()+" CI job を取得中...", term.AnsiDim, m.colored)}
	case !haveDetails:
		rows = []string{term.Paint("(CI job 情報なし)", term.AnsiDim, m.colored)}
	case len(jobs) == 0:
		rows = []string{term.Paint("(Check はありません)", term.AnsiDim, m.colored)}
	default:
		// panelCursor が見える範囲の job を切り出す (maxPanelJobs でスクロール)
		start := 0
		if m.panelCursor >= maxPanelJobs {
			start = m.panelCursor - maxPanelJobs + 1
		}
		endJob := min(start+maxPanelJobs, len(jobs))
		for i := start; i < endJob; i++ {
			mark := "  "
			if i == m.panelCursor {
				mark = term.CursorMark(m.colored)
			}
			row := mark + StatusGlyph(jobs[i].State, m.colored, m.spinner()) + " " + jobs[i].Name
			if suffix := m.jobTimeSuffix(jobs[i]); suffix != "" {
				row += term.Paint(" ("+suffix+")", term.AnsiDim, m.colored)
			}
			rows = append(rows, row)
		}
	}
	title := fmt.Sprintf(" CI jobs: %s %s ", commit.ShortSHA, commit.Subject)
	switch {
	case len(jobs) > 0 && m.panelCursor >= 0:
		title = fmt.Sprintf(" CI jobs: %s (%d/%d) %s ", commit.ShortSHA, m.panelCursor+1, len(jobs), commit.Subject)
	case len(jobs) > 0:
		title = fmt.Sprintf(" CI jobs: %s (%d 件) %s ", commit.ShortSHA, len(jobs), commit.Subject)
	}
	box := term.BuildShadowPanelBox(title, rows, width, m.colored)
	if m.detailOv.Visible() {
		// 詳細ボックスは job パネルの「子」であることが分かるよう段差を付ける (ユーザー要望)
		for _, line := range m.detailBoxLines(width - len(detailIndent)) {
			box = append(box, detailIndent+line)
		}
	}
	return box
}

// detailIndent は job 詳細ボックスのツリー段差 (job パネルの子であることの視覚表現)。
const detailIndent = "  "

// detailBoxLines は job 詳細ポップアップの描画行。job 名/cache キー/表示行数を解決して detailOv へ
// 渡す (diffBoxLines が commit を解決して diffOv へ渡すのと同型)。job パネルの直下へ重ねる。
func (m *browseModel) detailBoxLines(width int) []string {
	name := ""
	if job, ok := m.focusedJob(); ok {
		name = job.Name
	}
	return m.detailOv.BoxLines(width, m.colored, m.spinner(), name, m.detailKey(), m.visibleDetailRows())
}
