package main

import (
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/lineedit"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"

	"glogx/internal/app"

	tea "charm.land/bubbletea/v2"
)

// prefixMsg は tmux prefix の取得結果 (起動時に 1 回、非同期)。
type prefixMsg struct{ key string }

// inputEditsWith は、全画面の入力欄に打っている最中で、key がその入力欄の編集キーか (tmux prefix の分岐が使う)。
// F (treefiler) と L (mac-lan-manager) は入力欄にキャレットを出している間を「打っている最中」とする (CaretPos は入力欄があるときだけ ok)。
func (m *browseModel) inputEditsWith(key string) bool {
	if !isLineEditKey(key) {
		return false
	}
	switch m.activeFullScreen() {
	case FullScreenIssues:
		return m.issuesOv.TypingInput()
	case FullScreenFiler:
		_, _, ok := m.filerV.CaretPos()
		return ok
	case FullScreenLan:
		_, _, ok := m.lanV.CaretPos()
		return ok
	case FullScreenNone, FullScreenRatelimit, FullScreenDoctor, FullScreenStatus, FullScreenCount:
	}
	return false
}

// isLineEditKey は key が tuikit/lineedit の編集キー (移動・削除) か。lineedit の本物の Key に、使い捨ての行で聞く
// (語彙の表をここに写すと、lineedit にキーが増えたときに食い違う)。text を渡さないので文字の入力は数えない。
func isLineEditKey(key string) bool {
	var probe lineedit.Line
	return probe.Key(key, "")
}

func (m *browseModel) onPaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	// 貼り付け (bracketed paste。bubbletea v2 は既定で有効) は入力欄にだけ入れる。入力欄が無いときは捨てる:
	// キー操作として解釈すると、貼った文字列が 1 字ずつ一覧のキーとして走る (docs/glogx-ui-guide.md §7)。
	// 板が入力欄の上に出ている間 (action モーダル・再起動の確認) もキーは板が先に取るので、貼り付けも入れない
	if m.activeFullScreen() == FullScreenIssues && m.issuesOv.TypingInput() && !m.actModal.active() && !m.restartPromptVisible() {
		m.issuesOv.Paste(msg.Content)
	}
	if m.activeFullScreen() == FullScreenFiler && !m.actModal.active() && !m.restartPromptVisible() {
		m.filerV.Paste(msg.Content) // 入力中 (検索・! のコマンド) だけ入る。filer が捨てる
	}
	if m.activeFullScreen() == FullScreenLan && !m.actModal.active() && !m.restartPromptVisible() {
		m.lanV.Paste(msg.Content) // 入力中 (絞り込み・別名・帯) だけ入る。lanmgr が捨てる
	}
	return m, m.maybeTick()
}

func (m *browseModel) onKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// 高速連打やパイプ入力で複数の文字キーが 1 つのキーイベント (Text 長 > 1) に
	// まとまって届いた場合の分解。まとめずに msg.String() だけ見ると "hhq" のような
	// 未知キー扱いになり、以降の操作が全て無視されたように見える (pty スモークで実測)。
	// v2 の入力デコーダは grapheme クラスタ単位で 1 イベントを返すのでまとまらない想定だが、
	// 保険として残す (v1 で実際に起きた回帰であり、削っても得られる簡素化は 10 行)。
	if runes := []rune(msg.Text); len(runes) > 1 {
		var cmds []tea.Cmd
		for _, r := range runes {
			_, cmd := m.handleKey(string(r))
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			if m.done {
				break
			}
		}
		// 🚨 単キー経路と同じく maybeTick を束ねる: 分解したキーが出したトースト・glide は
		// 他に tick を回す理由が無ければ 1 フレームも進まず、shown=0 のまま凍って見えない。
		// この経路は「普段は通らないが通ったときだけ壊れる」ので気づきにくい。
		// maybeTick は single-flight なのでループ内の Cmd と重ねても二重には走らない。
		return m, tea.Batch(append(cmds, m.maybeTick())...)
	}
	// handleKey はキー経路の唯一の入口。ここで maybeTick を必ず束ねることで、ハンドラや
	// openX()/copyX() の内部で出したトースト (バリデーション失敗・コピー結果など) が
	// 呼び出し側で return m, nil されてもアニメ tick が確実に回る (トーストが shown=0 のまま
	// 凍って見えない事故を防ぐ)。maybeTick は single-flight で冪等なので二重には走らない。
	model, cmd := m.handleKey(msg.String())
	return model, tea.Batch(cmd, m.maybeTick())
}

// updateKeyReachable は C / X を update の入口として受けてよいかを返す。overlay が自分の語彙を
// 持っているとき (= ownsKeys) はそちらを優先する。今その状態を持つのは 3 つ:
// issues の絞り込み・URL ピッカー・目印確認 / status の pager・破棄確認 / **doctor の削除の確認と
// 実行中**。status viewer では X が「変更を捨てる」(docs/status-viewer-spec.md) なので渡す。
// それ以外 (ratelimit / diff / PR status / job パネル) は入力モードも C / X の割り当ても
// 持たないので常に受ける = どの画面からでも update を始められる (README のキー表が正本)。
// 🚨 overlay に新しい入力モード (y/N 確認など) や C / X の割り当てを足すときはここも直す。
// overlay 側は「未知キーは無視」なので、忘れても build もテストも壊れず、update が確認中のキーを
// 先取りする形で静かに壊れる (doctor に削除確認を足す予定が最初の該当)。
func (m *browseModel) updateKeyReachable(key string) bool {
	if m.issuesOv.Visible() && m.issuesOv.OwnsKeys() {
		return false
	}
	// doctor の削除は y/N の確認と実行中のブロックを持つ。ここで譲らないと
	// 確認中の X が codex update を始め、削除の確認が裏に残る
	if m.doctorOv.Visible() && m.doctorOv.OwnsKeys() {
		return false
	}
	if m.statusOv.Visible() && (m.statusOv.OwnsKeys() || key == "X") {
		return false
	}
	// treefiler は C を「経路以外を全部畳む」に使う (spec §0.1)。入力モード中は X も譲る (どちらも filer の Binds が決める)
	if m.filerV.Visible() && m.filerV.Binds(key) {
		return false
	}
	if m.lanV.Visible() && m.lanV.Binds(key) {
		return false // mac-lan-manager の入力中・確認・設定も全部のキーを譲る (lanmgr の Binds が決める)
	}
	return true
}

func (m *browseModel) handleKey(key string) (tea.Model, tea.Cmd) {
	m.keyed = true
	if m.swallowKeyRepeat(key) {
		return m, nil
	}
	// C-g は即終了: tmux の C-g popup (bind -n C-g) をトグル風に開閉するため
	// (開くキーと同じキーで閉じる)。
	// 閉じる演出の途中に来たキーは即着地させる (q を押してから消えるまでの間、入力が
	// 効かない時間を作らない)
	if m.zoom.closing() {
		m.zoom.finish()
		m.done = true
		return m, tea.Quit
	}
	// issues viewer の閉じる演出中に来たキーも即着地させる。🚨 ただしキーは飲み込まず、
	// 畳んだあとの状態で通常どおり処理する: 飲むと「q で閉じた直後の q が効かない」時間が
	// できる (アプリの終了演出はキーを飲んでも終わるだけなので失うものが無いが、こちらは違う)。
	// i ならこのあと下の分岐で開き直しになる = 閉じる演出の途中で開き直せる。
	//
	// 🚨 viewer へ routing する分岐 (下の issuesOv.visible()) より前に置くこと。後ろに置くと
	// 演出中のキーが viewer 側で処理され、モードを持つキー (/ の絞り込み・n の確認) が
	// 「畳んだ後の view」に状態を残す = 次に i で開いた瞬間に蘇る。
	//
	// 🚨 例外は e (エディタ) の 1 キーだけ。ここを素通ると、板がまだ見えているのに git log 一覧側の
	// e (openEditorAtRoot = `nvim .`) が全画面で起動し、「見ている issue を開いたつもりが repo root」
	// になる。上の「飲むと q が効かない窓ができる」は q/Esc の応答性の話で、e を 1 キーだけ捨てても
	// q は素通しのまま成立するので、両立させる (issues viewer の e が案内キーになった 2026-08-13
	// 以降、踏む確率が上がった)。
	closedIssues := m.issuesOv.FinishClose()
	closedStatus := m.statusOv.FinishClose() // status viewer も同じ契約 (閉じ演出中のキーは viewer に届かない)
	if (closedIssues || closedStatus) && key == "e" {
		return m, m.maybeTick()
	}
	if key == "ctrl+c" || key == "ctrl+g" {
		return m.handleQuitKey(key)
	}
	// git push/pull/update の確認と実行中ガードは action モーダルが捌く (通知はトースト)
	// (警告/結果の dismiss・確認 y/N・実行中のキー無視。判定順は actionModal.handleKey 側)。
	// 実行を伴う確認 y は action (実行 tea.Cmd) を載せて返すので maybeTick と束ねる。
	if consumed, action := m.actModal.handleKey(key); consumed {
		if action != nil {
			return m, tea.Batch(action, m.maybeTick())
		}
		return m, nil
	}
	// tmux prefix の誤爆フィードバック: popup 表示中は tmux がキーを処理しない
	// (display-popup はモーダル) ため、window 移動しようとした prefix はここへ素通りしてくる。
	// 無言だと「効かない」だけで理由が分からないので、prefix キー自体を飲んで案内を出す。
	//
	// 🚨 飲むのは prefix キー 1 つだけ。続くキーは通常の操作として処理する: prefix は
	// 押し間違いで押されるので、打ち直した入力まで失敗扱いにしない (ユーザー判断)。
	// 代償として popup 内の prefix+p は PR オープンを発火する。破壊的な b/u は y/N 確認が
	// 挟まるので許容した = 確認なし即実行のキーを増やすとこの判断が崩れる。
	//
	// 確認モーダル (y/N) 中はここへ来ない (上のモーダル処理が先): モーダル内では
	// 「y 以外の任意キー = キャンセル」というモーダルの語彙を優先する。
	// 対象キーはハードコードせず起動時に tmux サーバへ聞いた現在値 (prefixMsg)。
	// tmux 外や取得失敗では tmuxPrefix="" のままこの機能ごと無効になる。
	//
	// 🚨 入力欄に打っている間で、prefix が入力欄の編集キーでもある (C-a = 先頭へ / C-b = 1 字左へ) ときだけ飲まずに入力欄へ
	// 渡す。飲むと入力欄でその編集が効かない。編集キーでない prefix (C-t 等) は入力欄でも今までどおり飲んで案内する
	// (渡しても何も起きず、案内だけが消える)。C-b の利用者が window 移動のつもりで押すとキャレットが動くが、入力欄では
	// docs/glogx-ui-guide.md §7 の編集の語彙を優先する (案内を出して編集を捨てる方が、C-b を左へ使う人を壊す)。
	// 入力欄が自分で捌く移動のキー (ctrl+n / ctrl+p) は lineedit の編集キーではないので、prefix にしている人には入力中も
	// 案内に取られる。救うには入力欄のキーの表をここにもう 1 つ持つことになるので、稀な設定として採らない
	if m.tmuxPrefix != "" && key == m.tmuxPrefix && !m.inputEditsWith(key) {
		// 通知は右下トースト (中央ダイアログは操作を遮って重い)
		m.toast.Show("tmux prefix は popup では効きません (C-g で閉じてから)", false)
		return m, m.maybeTick()
	}
	// emacs 流の水平移動エイリアス (C-n/C-p = ↓/↑ は各ビューで対応済み)。ここで
	// 正規化するので全ビュー (一覧/パネル/詳細/diff) に一括で効く。
	// 🚨 C-b は ← の別名ではない (push を C-b → b に変えた名残で未割当)
	if key == "ctrl+f" {
		key = "right"
	}
	key = app.NormalizeSpaceKey(key)
	// バックグラウンドビルドの完成ダイアログ。r で確認なしに再起動する (ユーザー要望 2026-08-01)。
	//
	// 🚨 出ている間はキーを 1 つ飲んで必ず閉じる (r 以外は「今はしない」)。素通しにすると、
	// r が issues viewer の再読込・job パネルの再実行にも割り当たっているので「再読込のつもりが
	// 再起動」になる。🚨 viewer の分岐より前に置く: viewer は全画面モーダルで全キーを飲むため、
	// 後ろに置くと viewer を開いている間ダイアログに答えられない。
	//
	// 🚨 描画と同じ restartPromptVisible() で判定する (フラグを直接見ない)。出ていないダイアログに
	// キーを吸わせない・出ているのに届かない、の両方をこの一致で防ぐ。
	//
	// 🚨 ここに限っては restartPending を直接見ても今は同じ結果になる: 上の actModal.handleKey が
	// ownsKeys() のとき必ず consumed で return するので、この行に来た時点で ownsKeys() は false =
	// 2 つの述語は構造的に等価。つまりこの選択を守るテストは書けない (変異させても green)。
	// それでも述語を揃えておくのは、その等価性が「actModal を先に捌く」という並び順に依存して
	// いて、並び替えた瞬間に黙って壊れるため。テストで守られていない箇所だと分かるように明記する。
	if m.restartPromptVisible() {
		m.restartPending = false // 答えた = 保留を解除する (次のビルド完成まで出ない)
		if key == "r" {
			return m.restartForNewBinary()
		}
		return m, nil
	}
	// doctor 表示中も全画面: キーはここで飲み切る (rlDash と同じ位置・同じ理由)。
	// C = claude update / X = codex update (確認なし即実行。ユーザー選定 2026-07-22)。U=usage と並ぶ
	// 大文字の「Claude Code メタ操作」で、実行中は spinner モーダルを出す (描画は finishWithGlobalChrome
	// がどの画面にも重ねる)。🚨 overlay の分岐より前に置くこと: 後ろだと各 overlay がキーを飲み、
	// git log 一覧へ戻らないと update を始められない (ユーザー要望 2026-09-02: doctor / ratelimit /
	// issues / status の上からも開始したい)。overlay 自身の語彙が先に立つ場面だけ updateKeyReachable
	// が譲る (入力・確認モード中、status viewer の X = 変更を捨てる)。
	if target, ok := updateKeyTarget(key); ok && m.updateKeyReachable(key) {
		m.usageOv.Dismiss() // 一覧の他のキーと同じ語彙 (U で出し、次のキーで消える)
		return m, tea.Batch(m.actModal.startUpdateFor(target), m.maybeTick())
	}
	// 全画面ビューア (doctor / 残量ダッシュボード / issues / status) が出ている間は、キーを
	// そのビューアが飲み切る。🚨 **どれが出ているかの出典は activeFullScreen 1 箇所**。
	// ここで個別に visible() を並べ直さない。
	//
	// 🚨 この dispatch の位置は動かさないこと。前 (actModal の確認・実行中ガード / tmux prefix /
	// 再起動ダイアログ / C・X の update) へ動かすと viewer の語彙が外側に奪われ、後ろ (裸の
	// b / u = push / pull、U / R / D) へ動かすと viewer を見ている最中の u が
	// git pull --rebase の確認を開く footgun になる。
	switch m.activeFullScreen() {
	case FullScreenDoctor:
		return m.routeKeyToDoctor(key)
	case FullScreenRatelimit:
		return m.routeKeyToRatelimitDash(key)
	case FullScreenIssues:
		return m.routeKeyToIssues(key)
	case FullScreenStatus:
		return m.routeKeyToStatus(key)
	case FullScreenFiler:
		return m.routeKeyToFiler(key)
	case FullScreenLan:
		return m.routeKeyToLan(key)
	case FullScreenNone, FullScreenCount:
		// 一覧を見ている (FullScreenCount は番兵で activeFullScreen は返さない)。下へ落とす
	}
	// usage オーバーレイのトグル / dismiss。モーダル (push/pull 確認)・prefix・
	// 実行中ガードを素通りしないよう必ずそれらの後に置く: 先頭に置くと U が push 確認を
	// キャンセルし損ねて残った確認へ Enter で誤 push する footgun になる。
	// U は明示トグル (取得中なら spinner を回し直す)。それ以外のナビゲーションキーは「起動時
	// グランス」を引っ込める副作用だけ持たせ、キー本来の動作は下の dispatch/switch で続行する。
	if key == "U" {
		return m, m.toggleUsage()
	}
	// R = 全画面 ratelimit ダッシュボード。U と同じ判定位置に置く (モーダル・prefix・実行中
	// ガードの後) — 先頭に置くと push/pull 確認を素通りする footgun が同じ形で起きる。
	if key == "R" {
		return m, m.openFullScreen(FullScreenRatelimit)
	}
	// D = 全画面 doctor。R と同じ判定位置 (モーダル・prefix・実行中ガードの後)。
	// 開いた時にスキャンが始まる (起動時には走査しない)。
	if key == "D" {
		return m, m.openFullScreen(FullScreenDoctor)
	}
	// F = 全画面 treefiler。R / D と同じ判定位置なので、diff・PR status・job パネルの上からも開く
	if key == "F" {
		return m, m.openFullScreen(FullScreenFiler)
	}
	// L = 全画面 mac-lan-manager。F と同じ判定位置
	if key == "L" {
		return m, m.openFullScreen(FullScreenLan)
	}
	m.usageOv.Dismiss()
	// diff ポップアップ表示中はスクロール/閉じる操作だけを受ける (最前面のモーダル)
	if m.diffOv.Visible() {
		return m.handleDiffKey(key)
	}
	// PR 状態ポップアップ表示中も同様にモーダル (o/y/閉じるだけを受ける)
	if m.prStatusOv.Visible() {
		return m.handlePRStatusKey(key)
	}
	// i = issues viewer を開く (全画面。読む規約は docs/issues-viewer-spec.md)。一覧表示中
	// だけ受ける: job パネル/詳細を開いているときはそちらの語彙を優先する。初回だけ非同期で
	// スキャンし、以降は結果を保持したまま開閉する (再スキャンは viewer 内の r)。
	if key == "i" && m.panelSHA == "" {
		return m, m.openFullScreen(FullScreenIssues)
	}
	// s = status viewer を開く (全画面。規約は docs/status-viewer-spec.md)。i と同じく一覧表示中
	// だけ受ける: job パネル/詳細を開いているときはそちらの語彙を優先する。
	if key == "s" && m.panelSHA == "" {
		return m, m.openFullScreen(FullScreenStatus)
	}
	// b = push / u = pull --rebase (どちらも y/N 確認へ)。glogx の独自機能。
	// diff 表示中は b = 半ページ戻るなので、diff のディスパッチより後で拾う
	// (一覧/パネル/詳細では b/u は未使用。C-u の半ページ上とは別キー)
	if key == "b" {
		return m, m.confirmPush()
	}
	if key == "u" {
		m.actModal.askPull()
		return m, nil
	}
	// w = 直近の警告/エラーをクリップボードへコピー。トーストは数秒で消えるが
	// lastWarning は保持しているので消えた後でもコピーできる。ghErr (CI 取得失敗の sticky
	// 警告) は lastWarning に無くても hint に出続けているので fallback で拾う。tmux popup 内では
	// copy-mode に入れないため、pbcopy 直書きの extcmd.CopyToClipboard が唯一の取り出し口 (y/Y と同じ)。
	if key == "w" {
		warn := m.lastWarning
		if warn == "" && m.ghErr != nil {
			warn = m.ghErr.Warning()
		}
		if warn == "" {
			// コピーできる警告が無い旨は error トーストで出す (ユーザー要望 2026-07-23)。lastWarning は
			// 汚さない (この文言自体はコピー対象でないため showWarning は通さない)。
			m.toast.Show("コピーできる警告はありません", false)
			return m, m.maybeTick()
		}
		m.copyWithToast(warn, "警告をコピーしました")
		return m, m.maybeTick()
	}
	// q はビューのスタックを 1 段戻る (tig 流。ユーザー要望): 詳細 → job 一覧 →
	// コミット一覧、と閉じていき、最上位でだけ終了。即終了したいときは Ctrl-C
	if key == "q" {
		switch {
		case m.detailOv.Visible():
			m.detailOv.Close()
		case m.panelSHA != "":
			m.closePanel()
		default:
			return m.quit()
		}
		return m, nil
	}
	if m.panelSHA != "" {
		return m.handlePanelKey(key)
	}
	switch key {
	case "esc":
		return m.quit()
	case "enter", " ", "l", "right", "tab":
		return m, m.openPanel()
	case "y":
		m.copyFocusURL()
	case "p":
		return m, m.openPR()
	case "P":
		return m, m.openPRStatus()
	case "d":
		return m, m.openDiff()
	case "o":
		return m, m.openCommitURL()
	case "e":
		return m, m.openEditorAtRoot()
	default:
		// 移動の語彙 (j/k/g/G/半ページ) は listnav が持つ。🚨 この面は b が push (上で捌く) で
		// Space が開くなので、上半ページの経路は ctrl+u / pgup (shift+space は端末によって届かない。
		// 理由は term.PagerScrollKey の doc)
		return m, m.applyListMotion(listnav.MotionOf(key))
	}
	return m, nil
}

// applyListMotion はコミット一覧の移動を適用する。1 行移動はカーソル、半ページはビューポートを
// 動かす (1 コミットが複数行なので、カーソルは動かさず窓だけ送る)。どちらも prev からの距離を
// glide に載せる。端へのジャンプは即時 (距離が不定で glide の意味が薄い)。
func (m *browseModel) applyListMotion(mo listnav.Motion) tea.Cmd {
	prev := m.offset
	switch mo {
	case listnav.Down:
		m.cursor = app.ClampIdx(m.cursor+1, len(m.commits))
		m.ensureCursorVisible()
	case listnav.Up:
		m.cursor = app.ClampIdx(m.cursor-1, len(m.commits))
		m.ensureCursorVisible()
	case listnav.HalfDown:
		m.offset = m.clampOffset(m.offset + listnav.Half(m.pageSize()))
	case listnav.HalfUp:
		m.offset = m.clampOffset(m.offset - listnav.Half(m.pageSize()))
	case listnav.Top:
		m.cursor, m.offset = 0, 0
		m.glide.Stop()
		return nil
	case listnav.Bottom:
		m.cursor = app.ClampIdx(len(m.commits)-1, len(m.commits))
		m.ensureCursorVisible()
		m.glide.Stop()
		return nil
	case listnav.None:
		return nil
	}
	return m.startScrollAnim(prev)
}

// keyRepeatGuard は「押しっぱなし」を 1 回の入力として扱う判定窓。🚨 端末はキーを離した
// ことを教えてくれない (離鍵イベントは kitty のキーボード拡張が要り、glogx は要求していない。
// Update の KeyPressMsg の注記を参照) ので、離鍵の代わりに「同じキーが速く来続けたら
// 自動リピート」と時間で判定する。窓は押されるたびに更新するので、押し続けている限り 1 回に
// まとまり、指を離して窓が切れてから次の 1 回になる (ユーザー要望 2026-08-01)。
//
// 250ms の根拠 (このマシンの実測 2026-08-01/2026-08-07): 最初のリピートまで 225ms・以降 30ms
// 間隔 (defaults read -g InitialKeyRepeat=15 / KeyRepeat=2)。1 回目のリピートも窓に収める必要が
// あるので 225ms より長く取り、意識して 2 回押す間隔よりは短く保つ。当初 300ms だったが再打鍵の
// 待ちを縮めたいとの要望 (2026-08-07。200ms 希望だったが 225ms を覆えないため 225ms を超える
// 最小刻みの 250ms で合意)。🚨 InitialKeyRepeat を 16 (240ms) 以上へ変えるとこの窓を素通りして
// 長押しが 2 回 toggle に戻る。その時はここも追従させること。
const keyRepeatGuard = 250 * time.Millisecond

// repeatGuardedKeys は自動リピートを潰すキー。
//
// 🚨 移動系 (j/k/矢印/半ページ) は入れない: 押しっぱなしでスクロールし続けるのは期待される
// 動作で、潰すと「押しても動かない」壊れ方になる。潰すのは「開いて閉じる」を繰り返してしまう
// トグルと、押すたびに subprocess やファイル操作が走るキーだけ。
var repeatGuardedKeys = map[string]bool{
	"i": true, // issues viewer (押しっぱなしで高速に開閉する。ユーザー報告 2026-08-01)
	"s": true, // status viewer (i と同じ toggle。ユーザー報告 2026-08-07)
	"U": true, // usage オーバーレイ (開くたびに取り直しの判定が走る)
	"R": true, // ratelimit ダッシュボード (U と同じ toggle。開くたびに取得判定が走る)
	"D": true, // doctor (開くたびにスキャンが走る。押しっぱなしで開閉と走査を繰り返す)
	"d": true, // diff ポップアップ
	"P": true, // PR 状態ポップアップ
	"n": true, // next へ移動の確認 (開いた確認を次のリピートが閉じてしまう)
	"a": true, // 状態フィルタの巡回 (3 段なので押しっぱなしだと今どこか分からなくなる)
}

// swallowKeyRepeat は自動リピートとみなしたキーを飲む (true = 何もしない)。
func (m *browseModel) swallowKeyRepeat(key string) bool {
	// 🚨 入力欄に打っている間は抑止しない: 印字キーはすべて検索語で、https の ss のように同じ字を素早く続けて打つと
	// 2 字目が飲まれていた。y/N の確認 (n の目印) は入力欄ではないので抑止を残す (n のリピートが確認を取り消すため)
	if m.activeFullScreen() == FullScreenIssues && m.issuesOv.TypingInput() {
		return false
	}
	if m.activeFullScreen() == FullScreenFiler && m.filerV.OwnsKeys() {
		return false // treefiler の検索・! の入力中も同じ (ss / dd の 2 字目が飲まれていた。レビューの指摘 2026-10-09)
	}
	if m.activeFullScreen() == FullScreenLan && m.lanV.OwnsKeys() {
		return false // mac-lan-manager の絞り込み・別名の入力中も同じ
	}
	if !repeatGuardedKeys[key] {
		m.lastKey, m.lastKeyAt = "", time.Time{} // 別のキーが来たら押しっぱなしは切れている
		return false
	}
	now := app.TimeNow()
	repeat := key == m.lastKey && now.Sub(m.lastKeyAt) < keyRepeatGuard
	// 🚨 飲んだときも基準を更新する: これが「離すまで 1 回扱い」の実体で、押し続けている限り
	// 窓が伸び続ける。更新しないと keyRepeatGuard ごとに 1 回ずつ通ってしまう。
	m.lastKey, m.lastKeyAt = key, now
	return repeat
}
