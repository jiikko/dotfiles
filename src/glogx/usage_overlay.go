package main

import (
	"context"
	"errors"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"strconv"
	"strings"
	"time"

	"ratelimit/usage"

	tea "charm.land/bubbletea/v2"
)

// usageMsg は /usage の非同期取得結果 (右上オーバーレイ用)。2 つの形がある:
//   - part != nil: 出所 (Claude / codex) ごとの 1 本の結果。1 回の取得で 2 通届き、届いた方から表示へ入る (issue 626)
//   - part == nil: その周の結果を丸ごと持つ (ディスクキャッシュの hit。snap / err)
type usageMsg struct {
	snap *usage.Snapshot
	err  error
	part *usage.Part
}

// usageRound は走行中の 1 回の取得 (Claude と codex の 2 本) の途中経過。
type usageRound struct {
	base  *usage.Snapshot // 周の始まりの表示 (last-good の出典。nil = まだ何も取れていない)
	parts []usage.Part    // 届いた順
}

// usageRoundSources は 1 回の取得で投げる出所 (表示名は usageSourceName)。
var usageRoundSources = []string{"", usage.SourceCodex}

func usageSourceName(src string) string {
	if src == usage.SourceCodex {
		return "codex"
	}
	return "Claude Code"
}

// shown は届いた分を base に入れた表示を返す。何も取れていない (base が nil で、届いた分が全部失敗) なら nil
// (取得中の表示のまま待つ。Claude の失敗だけで「Claude 枠の無い空の snap」を出さない)。
func (r usageRound) shown() *usage.Snapshot {
	ok := r.base != nil
	for _, p := range r.parts {
		ok = ok || p.Err == nil
	}
	if !ok {
		return nil
	}
	s := r.base
	for _, p := range r.parts {
		s = s.With(p)
	}
	return s
}

func (r usageRound) arrived(src string) bool {
	for _, p := range r.parts {
		if p.Source == src {
			return true
		}
	}
	return false
}

// usageOverlay は Claude Code / codex の残量を右上に重ねるオーバーレイの状態と描画。
// browseModel から usage の関心事 (状態 + fetch/toggle/render) を 1 つの型へ切り出した
// サブコンポーネント。取得ロジック自体は bubbletea 非依存の usage パッケージにあり、こちらは
// overlay の UI 状態機械 (bubbletea 結合のため glogx 側に置く)。browseModel は 1 フィールド
// (usageOv) だけを持ち、キー/メッセージ/描画をこの型へ委譲する。
type usageOverlay struct {
	visible bool            // 表示中か (起動時 true = 起動時グランス表示)
	snap    *usage.Snapshot // 取得済みの /usage スナップショット (nil = 取得中)
	err     error           // 取得失敗 (表示は "取得失敗" に落とす)
	// cancel は fetch 専用の cancel。quit で走行中の subprocess を中断する。browseModel の
	// CI fetch 用 cancel とは別立て: 共有すると CI fetch 完了時の defer cancel() が走行中の
	// usage fetch を巻き添えキャンセルして "取得失敗" に落ちる (レビュー指摘 2026-07-21)。
	cancel context.CancelFunc
	// inFlight は fetch の single-flight ガード (fetchCmd で立て、結果の handle で降ろす)。
	// cancel が単一スロットのため、fetch を overlap させると先行分の cancel を上書きで取りこぼし、
	// quit 後も先行 subprocess が fetchTimeout まで残る。定期リフレッシュ同士は
	// usageRefreshInterval > fetchTimeout で overlap しない (tui.go の定数コメント) が、
	// U 再表示 (toggleUsage の stale 経路) との重なりはこのガードでしか防げない。
	inFlight bool
	// fetchedAt は snap を取得した時刻 (zero = 未取得)。非表示中はリフレッシュを止めるので、
	// 再表示時に「今の表示が古いか」を判断する出典として要る (stale 参照)。
	fetchedAt time.Time
	// staleErr は last-good を保持したまま失敗した最後の取得の理由 (nil = 表示中の snap が最新の取得)。
	// 全滅時も前回の枠を出し続けるので、これが無いと古い値が黙って表示される。
	staleErr error
	// round は走行中の取得の途中経過 (inFlight の間だけ意味を持つ)。
	round usageRound
	// shape は場所取りの枠の形の出典 (前回のキャッシュの枠。最初の取得を始めるときに読む。view)。
	shape []usage.Window
}

// fetchCmd は Claude Code の /usage と codex の rateLimits を非同期取得する tea.Cmd。
// 2 本を同時に投げ (Cmd が tea.BatchMsg を返す)、出所ごとに usageMsg が届く。速い方 (codex app-server は
// 0.6〜1.2s) を遅い方 (claude subprocess は 1 回 ≈ 2.0s wall / 1.8s CPU。実測 2026-07-25。支配的なのは node 起動 +
// Claude Code セッション初期化で、/usage の内部処理は 462ms) に待たせない (issue 626)。どちらもトークン課金は
// 発生しない。初期描画のクリティカルパスには乗せない。cancel を保持し、quit 時に走行中の
// subprocess を中断できるようにする (fast-quit での子プロセスのオーファン化を防ぐ)。起動時に 1 回 + 以降 usageRefreshInterval ごとにバックグラウンド再取得で呼ばれる
// (U トグルは再 fetch しない)。定期リフレッシュ中も表示は last-good を保つ (handle 参照)。
//
// useCache=true (起動時) は fresh なディスクキャッシュがあれば subprocess を起こさず即答する。
// 定期リフレッシュ側は false — 鮮度を作るのがその役目なので、自分が書いたキャッシュを読み返す
// のは無意味 (TTL == 周期なので必ず miss する)。キャッシュの保存は周の終わりに handle が行う。
func (o *usageOverlay) fetchCmd(useCache bool) tea.Cmd { return o.fetchCmdWith(useCache, false) }

// usageFooter は箱の末尾の文言。表示は usageRefreshInterval ごとに読み直すが、Claude の枠そのものは
// 全プロセス共有のゲートの間隔 (usage.SharedFresh) でしか新しくならないので、そちらを出す (issue 627)。
var usageFooter = strconv.Itoa(int(usage.SharedFresh/time.Minute)) + "分ごとに更新"

// fetchNowCmd は人が「今すぐ取り直す」を押したときの取得。Claude 側は全プロセス共有のゲートの
// 5 分の間引きを飛ばす (usage.FetchClaudePartNow。間引きのまま返すと、数分前の値が今取った値として入る)。
func (o *usageOverlay) fetchNowCmd() tea.Cmd { return o.fetchCmdWith(false, true) }

func (o *usageOverlay) fetchCmdWith(useCache, force bool) tea.Cmd {
	if o.inFlight {
		return nil // 走行中の fetch がある: overlap させない (inFlight フィールドの doc)
	}
	o.inFlight = true
	// 取り消し (stop) は今持つが、timeout の時計は走り出してから刻む: 起動時の取得は CI の取得が終わるまで
	// 預けられる (issue 570) ので、ここで刻み始めると預けている間に持ち時間を失う
	parent, cancel := context.WithCancel(context.Background())
	o.cancel = cancel
	// last-good の出典は周の始まりの表示。束縛は UI スレッドのここで行う
	o.round = usageRound{base: o.snap}
	if o.fetchedAt.IsZero() && o.shape == nil {
		o.shape = loadUsageShape() // 場所取りを出すのは最初の取得だけ (waitingSources)。小さなファイルを 1 回読むだけ
	}
	one := func(fetch func(context.Context) usage.Part) tea.Cmd {
		return func() tea.Msg {
			ctx, cancelTimeout := context.WithTimeout(parent, fetchTimeout)
			defer cancelTimeout()
			p := fetch(ctx)
			return usageMsg{part: &p}
		}
	}
	claude := usage.FetchClaudePart
	if force {
		claude = usage.FetchClaudePartNow
	}
	both := tea.BatchMsg{one(claude), one(usage.FetchCodexPart)}
	return func() tea.Msg {
		// キャッシュ経路の失敗 (path 解決不能・破損・TTL 切れ) はすべて「キャッシュなし」に
		// 落として通常取得へ進む (キャッシュ都合で usage 表示を失わない)
		if useCache {
			if path, err := usageCachePath(); err == nil {
				if snap, ok := loadUsageCache(path, time.Now()); ok {
					return usageMsg{snap: snap}
				}
			}
		}
		return both
	}
}

// showCached はディスクキャッシュが当たれば、fork せずにその場で表示へ入れる (当たったか返す)。
// fetchCmd(true) のキャッシュ経路と同じ判定・同じ格納 (handle) を通す。
func (o *usageOverlay) showCached(now time.Time) bool {
	path, err := usageCachePath()
	if err != nil {
		return false
	}
	snap, ok := loadUsageCache(path, now)
	if !ok {
		return false
	}
	o.handle(usageMsg{snap: snap})
	return true
}

// endRound は走行中の取得を閉じる (次の fetchCmd を受け付け、ctx を解放する)。
func (o *usageOverlay) endRound() {
	o.inFlight = false
	o.round = usageRound{}
	if o.cancel != nil {
		o.cancel()
		o.cancel = nil
	}
}

// handle は取得結果 (usageMsg) を格納する。返す Cmd は周の終わりのキャッシュの保存 (無ければ nil)。
//
// 不変条件: 一度取れた usage 表示は、定期リフレッシュの一時的な失敗では失わない。出所ごとに
// last-good を持ち (usage.Snapshot.With)、両方とも失敗した周は表示をそのまま保って理由を staleErr に置く
// (1 分ごとの再取得が回線瞬断等でたまに転けても、右上の残量表示がチラつかない)。初回取得の
// 全滅 (snap 未取得) はそのままエラー表示する。1 本でも取れたら err を下ろし (初回失敗からの回復)、
// staleErr は 1 本でも取れた周の終わりに下ろす。
//
// 🚨 usageMsg は周の番号を持たない。周を閉じる (endRound) のは、2 本がそろったときとキャッシュの hit
// (part を投げない) だけなので、閉じた後に part が届く経路は今は無い。周の途中で showCached や part の無い
// usageMsg を入れる経路を足すなら、周の番号を持たせてから足す (敵対的レビュー 2026-10-02 の記録)。
func (o *usageOverlay) handle(msg usageMsg) tea.Cmd {
	if msg.part == nil {
		o.endRound()
		if msg.err != nil && o.snap != nil {
			// 定期リフレッシュの一時失敗: last-good を保持し表示を崩さない (古いことは注記で伝える)
			o.staleErr = msg.err
			return nil
		}
		o.staleErr = nil
		o.snap = msg.snap
		o.err = msg.err
		if msg.err == nil {
			o.fetchedAt = timeNow()
		}
		return nil
	}
	o.round.parts = append(o.round.parts, *msg.part)
	if msg.part.Err == nil {
		// 初回の失敗からの回復は届いた時点で出す。staleErr (前回の値を表示中) は周の終わりまで下ろさない:
		// まだ届いていない出所の枠は前回の値のままなので、先に届いた方で注記を消すと古い値が黙って出る
		o.err = nil
	}
	if s := o.round.shown(); s != nil {
		o.snap = s
	}
	if len(o.round.parts) < len(usageRoundSources) {
		return nil
	}
	parts, base := o.round.parts, o.round.base
	o.endRound()
	var errs []error
	fresh := (*usage.Snapshot)(nil)         // この周で取れた分だけ (キャッシュへ保存する値。last-good を混ぜない)
	for _, src := range usageRoundSources { // 理由は届いた順でなく Claude → codex の順に並べる
		for _, p := range parts {
			if p.Source != src {
				continue
			}
			fresh = fresh.With(p)
			if p.Err != nil {
				errs = append(errs, p.Err)
			}
		}
	}
	if len(errs) == len(parts) {
		err := errors.Join(errs...)
		if base != nil {
			o.snap, o.staleErr = base, err
			return nil
		}
		o.snap, o.err = nil, err
		return nil
	}
	o.staleErr = nil
	o.fetchedAt = timeNow()
	// キャッシュは今回取れた Claude 枠を完全性の必須条件にする (Claude 必須・codex best-effort。usage_cache.go)
	if !fresh.HasClaude() {
		return nil
	}
	return func() tea.Msg {
		if path, err := usageCachePath(); err == nil {
			_ = saveUsageCache(path, fresh, time.Now()) // best-effort: 保存失敗でも表示は成立させる
		}
		return nil
	}
}

// waitingSources は、まだ届いていない出所 (場所取りを置く対象)。最初の取得が終わるまでだけ返す
// (その間の表示には届いた出所の枠しか無い): 定期リフレッシュは静かに差し替える (下の boxLines のフッターの注記) ので、
// codex 未導入の環境で毎分 codex の場所取りが出入りしないようにする。
func (o *usageOverlay) waitingSources() []string {
	if !o.inFlight || o.snap == nil || !o.fetchedAt.IsZero() {
		return nil
	}
	srcs := make([]string, 0, len(usageRoundSources))
	for _, src := range usageRoundSources {
		if !o.round.arrived(src) {
			srcs = append(srcs, src)
		}
	}
	return srcs
}

// waiting は waitingSources の表示名。
func (o *usageOverlay) waiting() []string {
	srcs := o.waitingSources()
	names := make([]string, 0, len(srcs))
	for _, src := range srcs {
		names = append(names, usageSourceName(src))
	}
	return names
}

// view は描く Snapshot (U の箱と R の盤の両方がこれを描く。spinner は場所取りに添えるスピナーの今のコマ)。まだ届いていない出所には場所取りの枠
// (usage.PendingWindows) を入れ、届いた後とレイアウト (行の数・列の幅・盤の段) を揃える (issue 626)。
// 場所取りは表示だけのもので、o.snap には入れない (キャッシュ・last-good の出典を汚さない)。
func (o *usageOverlay) view(spinner string) *usage.Snapshot {
	s := o.snap
	// 🚨 Claude の場所取りを With に「成功」として入れると ClaudeErr を消すが、待っている間の snap は ClaudeErr を
	// 持たない (Claude が失敗したら届いた扱いで、場所取りを置かない)
	for _, src := range o.waitingSources() {
		ws := usage.PendingWindows(src, o.shape)
		for i := range ws {
			ws[i].Spinner = spinner
		}
		s = s.With(usage.Part{Source: src, Windows: ws})
	}
	return s
}

// waitingNote は waiting を 1 行にした表示 (無ければ空)。
func (o *usageOverlay) waitingNote(spinner string) string {
	names := o.waiting()
	if len(names) == 0 {
		return ""
	}
	return spinner + " " + strings.Join(names, " / ") + " 取得中..."
}

// stale は今の表示が許容陳腐度 (usageRefreshInterval) を超えているか。未取得も stale 扱い。
// 非表示中はリフレッシュを止めるため、再表示 (U) のときにこれで取り直しを判断する。
func (o *usageOverlay) stale() bool {
	return o.fetchedAt.IsZero() || timeNow().Sub(o.fetchedAt) >= usageRefreshInterval
}

// toggle は U キーで表示/非表示を反転する。
func (o *usageOverlay) toggle() { o.visible = !o.visible }

// dismiss は任意のナビゲーションキーで起動時グランス表示を引っ込める。
func (o *usageOverlay) dismiss() { o.visible = false }

// loading は取得待ち (spinner を回す) かどうか。表示中かつ、結果未着 (snap も err も無い) か片方の出所を
// 待っている (場所取りの行のスピナー) ときだけ true。これが true の間だけ tick を回してスピナーを animate する。
func (o *usageOverlay) loading() bool {
	return o.visible && o.awaiting()
}

// awaiting は表示 (U の箱 / R のダッシュボード) にスピナーが要るか (片方を待つ間も場所取りのスピナーを回す)。
func (o *usageOverlay) awaiting() bool {
	return (o.snap == nil && o.err == nil) || len(o.waiting()) > 0
}

// stop は quit 時に走行中の usage fetch subprocess を cancel する (オーファン化防止)。
func (o *usageOverlay) stop() {
	if o.cancel != nil {
		o.cancel()
	}
}

// boxLines は右上オーバーレイの複数行モーダル (影付き枠) を組み立てる。非表示なら nil。
// 取得中は枠内でスピナー (呼び出し側が現在フレームを渡す) を回し、失敗時は理由、成功時は
// 枠ごとに 1 行整列表示 + 末尾に自動更新の明示フッターを添える。spinner / colored / width は
// browseModel 側の状態を受け取る (この型は bubbletea の tick や端末幅を直接知らず、描画に
// 必要な値だけを引数で受ける)。
func (o *usageOverlay) boxLines(width int, colored bool, spinner string) []string {
	if !o.visible {
		return nil
	}
	// 取得中/失敗でも省略しない "Claude Code · usage" を出す (ユーザー要望 2026-07-23)。箱幅は
	// 下でこのタイトルが切り詰められない幅を最低確保する。
	title := " Claude Code · usage "
	var rows []string
	switch {
	case o.err != nil:
		rows = []string{paint("取得失敗", ansiDim, colored)}
	case o.snap == nil:
		rows = []string{paint(spinner+" 取得中...", ansiDim, colored)}
	default:
		snap := o.view(spinner)
		// CLI バージョンが取れていればタイトルに添える (取得失敗時は空で従来どおり)。
		// バージョン文字列は外部バイナリの出力なので無害化して枠へ載せる
		if v := sanitizePlainLine(snap.Version); v != "" {
			title = " Claude Code v" + v + " · usage "
		}
		// codex の枠が取れているときだけ "+ codex" を添える (codex 未導入環境や取得失敗時に
		// 名前だけ出さない。行側の cx ラベルと対で、この箱が両 CLI の残量であることを示す)。
		// バージョンは Claude 側と同じく取れていれば添える (取得失敗時は名前だけで従来どおり)。
		if snap.HasCodex() {
			cx := " + codex"
			if v := sanitizePlainLine(snap.CodexVersion); v != "" {
				cx += " v" + v
			}
			title = strings.Replace(title, " · usage ", cx+" · usage ", 1)
		}
		// ヘッダー (列見出し) は自明なので表示しない (ユーザー要望 2026-07-23)。data 行のみ。
		// Claude と codex の境目には content 幅の区切り罫線を挟む (ユーザー要望 2026-07-31)。
		// 罫線幅を全グループの最大行幅に合わせるため、先に幅 w を確定してから組む。
		_, groups := usage.RenderTableGroups(snap, time.Now(), colored)
		w := 0
		for _, g := range groups {
			for _, r := range g {
				w = max(w, dispWidth(r))
			}
		}
		for gi, g := range groups {
			if gi > 0 {
				rows = append(rows, paint(strings.Repeat("─", w), ansiDim, colored))
			}
			rows = append(rows, g...)
		}
		// 自動更新の明示フッターを content 幅に右寄せで添える (ユーザー要望)。値の取得は静かに
		// 差し替わるので、更新中であることは出さない。
		if note := fetchNote(snap, o.staleErr); note != "" {
			// 右上の小さな箱を理由の長さで横に広げない (全文は R のダッシュボードに出る)
			rows = append(rows, paint(clipToWidth(note, max(w, dispWidth(title))), ansiYellow, colored))
		}
		footer := usageFooter
		rows = append(rows, padSpaces(max(w-dispWidth(footer), 0))+paint(footer, ansiDim, colored))
	}
	// 枠幅 = 内容の最大表示幅 + 罫線・影の余白。ただし title を切り詰めない幅 (title 幅 + 3。
	// buildShadowPanelBox が title を fw-2=boxWidth-3 に truncate するため) を最低確保する。
	// 端末幅は超えない。
	inner := 0
	for _, r := range rows {
		inner = max(inner, dispWidth(r))
	}
	boxWidth := min(max(inner+layout.PanelChrome, dispWidth(title)+3), width)
	return buildShadowPanelBox(title, rows, boxWidth, colored)
}

// overlayBoxTopRight は box をウィンドウ上部の右端へ重ねる (usage オーバーレイ用)。
func overlayBoxTopRight(window, box []string, width int, colored bool) []string {
	return layout.OverlayRight(window, box, width, colored, 0)
}

// overlayBoxBottomRight は box を window の下端 (末尾 len(box) 行 = hint 行の直上)・右端へ重ねる
// (トースト用)。
func overlayBoxBottomRight(window, box []string, width int, colored bool) []string {
	return layout.OverlayRight(window, box, width, colored, max(len(window)-len(box), 0))
}

// fetchNote は表示中の枠に古い値が混じっているときの注記を返す (無ければ空)。古い値が混じるのは
// 2 通り: 全滅して last-good を保持した (staleErr。handle) / Claude 側だけ失敗して前回の枠が残った
// (ClaudeErr。usage.Snapshot.With)。注記が無いと古い残量が黙って出続ける。
// 理由は外部コマンドの stderr を含むので無害化して載せる。
func fetchNote(snap *usage.Snapshot, staleErr error) string {
	if snap == nil {
		return ""
	}
	// 全滅の注記を優先する: last-good 自体が Claude を前回値で補った snap でも、「表示中の値は全部古い」が
	// 上位の事実で、1 行に 2 つの注記は載せない
	if staleErr != nil {
		// 全滅の理由は errors.Join (改行区切り。handle)。無害化は改行を詰めて落とすので、先に区切りへ置き換える
		return "🚨 取得に失敗 (前回の値を表示中): " + sanitizePlainLine(strings.ReplaceAll(staleErr.Error(), "\n", " / "))
	}
	if snap.ClaudeErr == "" {
		return ""
	}
	head := "🚨 Claude Code の取得に失敗"
	if snap.HasClaude() {
		head += " (前回の値を表示中)"
	}
	return head + ": " + sanitizePlainLine(snap.ClaudeErr)
}
