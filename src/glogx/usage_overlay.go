package main

import (
	"context"
	"strings"
	"time"

	"glogx/usage"

	tea "charm.land/bubbletea/v2"
)

// usageMsg は /usage の非同期取得結果 (右上オーバーレイ用)。
type usageMsg struct {
	snap *usage.Snapshot
	err  error
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
}

// fetchCmd は Claude Code の /usage と codex の rateLimits を非同期取得する tea.Cmd
// (usage.FetchAll が両者を並列取得し 1 Snapshot に併合。codex 側は失敗しても Claude 表示を
// 崩さない)。どちらもトークン課金は発生しないが、claude subprocess は 1 回 ≈ 2.0s wall /
// 1.8s CPU と重い (実測 2026-07-25。支配的なのは node 起動 + Claude Code セッション初期化で、
// /usage の内部処理は 462ms。codex app-server は 0.6〜1.2s で並列のため所要は claude 側が
// 支配)。初期描画のクリティカルパスには乗せない。cancel を保持し、quit 時に走行中の
// subprocess を中断できるようにする (fast-quit での子プロセスのオーファン化を防ぐ)。起動時に 1 回 + 以降 usageRefreshInterval ごとにバックグラウンド再取得で呼ばれる
// (U トグルは再 fetch しない)。定期リフレッシュ中も表示は last-good を保つ (handle 参照)。
//
// useCache=true (起動時) は fresh なディスクキャッシュがあれば subprocess を起こさず即答する。
// 定期リフレッシュ側は false — 鮮度を作るのがその役目なので、自分が書いたキャッシュを読み返す
// のは無意味 (TTL == 周期なので必ず miss する) 。取得結果は Claude 枠を含む場合だけキャッシュへ
// 書く (Claude 必須・codex best-effort)。片側失敗の last-good 補完前に保存し、古い枠の TTL を
// 新しい取得時刻で延命しない。
func (o *usageOverlay) fetchCmd(useCache bool) tea.Cmd {
	if o.inFlight {
		return nil // 走行中の fetch がある: overlap させない (inFlight フィールドの doc)
	}
	o.inFlight = true
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	o.cancel = cancel
	// last-good 補完用の前回結果。closure の生成は UI スレッドなのでここで束縛する
	// (goroutine から o.snap を読むとデータレース)。
	prev := o.snap
	return func() tea.Msg {
		defer cancel()
		// キャッシュ経路の失敗 (path 解決不能・破損・TTL 切れ) はすべて「キャッシュなし」に
		// 落として通常取得へ進む (キャッシュ都合で usage 表示を失わない)
		path, pathErr := usageCachePath()
		if useCache && pathErr == nil {
			if snap, ok := loadUsageCache(path, time.Now()); ok {
				return usageMsg{snap: snap}
			}
		}
		snap, err := usage.FetchAll(ctx)
		if err == nil {
			// FetchAll は片側 (claude / codex) の一時失敗を err=nil で返すため、欠けた出所の
			// 枠を前回結果から補完する。ただしキャッシュは今回取得できた Claude 枠だけを完全性の
			// 必須条件とし、補完前に保存する。補完後に保存すると古い片側の枠が延命され続ける。
			if pathErr == nil && snap.HasClaude() {
				_ = saveUsageCache(path, snap, time.Now()) // best-effort: 保存失敗でも表示は成立させる
			}
			snap.MergeLastGood(prev)
		}
		return usageMsg{snap: snap, err: err}
	}
}

// handle は取得結果 (usageMsg) を格納する。
//
// 不変条件: 一度取れた usage 表示は、定期リフレッシュの一時的な失敗では失わない。既に
// スナップショットがある状態で失敗結果が来たら last-good を保持し "取得失敗" へ落とさない
// (1 分ごとの再取得が回線瞬断等でたまに転けても、右上の残量表示がチラつかない)。初回取得の
// 失敗 (snap 未取得) はそのままエラー表示する。リフレッシュ成功は last-good を新値へ置き換え、
// 初回失敗からの回復 (err クリア) も担う。
// 片側 (claude / codex) だけの失敗は err=nil で来るためこのガードでは受けられず、fetchCmd 側の
// MergeLastGood が出所単位で同じ不変条件を守る (二層で一対)。
func (o *usageOverlay) handle(msg usageMsg) {
	o.inFlight = false // fetchCmd の closure は成否によらず必ず usageMsg を返す (ここで対に降ろす)
	if msg.err != nil && o.snap != nil {
		return // 定期リフレッシュの一時失敗: last-good を保持し表示を崩さない
	}
	o.snap = msg.snap
	o.err = msg.err
	if msg.err == nil {
		o.fetchedAt = timeNow()
	}
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

// loading は取得待ち (spinner を回す) かどうか。表示中かつ結果未着 (snap も err も無い) の
// ときだけ true。これが true の間だけ tick を回してスピナーを animate する。
func (o *usageOverlay) loading() bool {
	return o.visible && o.snap == nil && o.err == nil
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
		// CLI バージョンが取れていればタイトルに添える (取得失敗時は空で従来どおり)。
		// バージョン文字列は外部バイナリの出力なので無害化して枠へ載せる
		if v := sanitizePlainLine(o.snap.Version); v != "" {
			title = " Claude Code v" + v + " · usage "
		}
		// codex の枠が取れているときだけ "+ codex" を添える (codex 未導入環境や取得失敗時に
		// 名前だけ出さない。行側の cx ラベルと対で、この箱が両 CLI の残量であることを示す)。
		if o.snap.HasCodex() {
			title = strings.Replace(title, " · usage ", " + codex · usage ", 1)
		}
		// ヘッダー (列見出し) は自明なので表示しない (ユーザー要望 2026-07-23)。data 行のみ。
		// Claude と codex の境目には content 幅の区切り罫線を挟む (ユーザー要望 2026-07-31)。
		// 罫線幅を全グループの最大行幅に合わせるため、先に幅 w を確定してから組む。
		_, groups := usage.RenderTableGroups(o.snap, time.Now(), colored)
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
		footer := "1分ごとに更新"
		rows = append(rows, padSpaces(max(w-dispWidth(footer), 0))+paint(footer, ansiDim, colored))
	}
	// 枠幅 = 内容の最大表示幅 + 罫線・影の余白。ただし title を切り詰めない幅 (title 幅 + 3。
	// buildShadowPanelBox が title を fw-2=boxWidth-3 に truncate するため) を最低確保する。
	// 端末幅は超えない。
	inner := 0
	for _, r := range rows {
		inner = max(inner, dispWidth(r))
	}
	boxWidth := min(max(inner+shadowBoxChrome, dispWidth(title)+3), width)
	return buildShadowPanelBox(title, rows, boxWidth, colored, ansiDim)
}

// overlayBoxRight は複数行の box を window の右端へ矩形で重ねる (右揃え)。base は載せ始める行
// (0=上端 / 下端は max(len(window)-len(box),0))。box の各行は buildPanelBox で幅が揃っているため
// 右端に清潔な長方形として載る。覆われる各行の左側 (見えている部分) は truncateKeepANSI で色を
// 保ったまま切り、境界で reset を挟んで開いた色/bg を閉じる (取得中に上部行の色が抜ける不具合の
// 修正)。box 行自身の色はそのまま活きる。⚠️ この色にじみ防止の合成ロジックの単一情報源 —
// TopRight/BottomRight で二重持ちして片方だけ退行させないため 1 箇所に集約している。
func overlayBoxRight(window, box []string, width int, colored bool, base int) []string {
	if len(window) == 0 || width <= 0 || len(box) == 0 {
		return window
	}
	reset := ""
	if colored {
		reset = ansiReset // 左側の開いた色/bg を box の直前で閉じる
	}
	for i, row := range box {
		pos := base + i
		if pos >= len(window) {
			break
		}
		bw := dispWidth(row)
		if bw >= width {
			window[pos] = clipToWidth(row, width)
			continue
		}
		leftWidth := width - bw
		left := truncateKeepANSI(window[pos], leftWidth)
		pad := padSpaces(max(leftWidth-dispWidth(left), 0))
		window[pos] = left + reset + pad + row
	}
	return window
}

// overlayBoxTopRight は box をウィンドウ上部の右端へ重ねる (usage オーバーレイ用)。
func overlayBoxTopRight(window, box []string, width int, colored bool) []string {
	return overlayBoxRight(window, box, width, colored, 0)
}

// overlayBoxBottomRight は box を window の下端 (末尾 len(box) 行 = hint 行の直上)・右端へ重ねる
// (トースト用)。
func overlayBoxBottomRight(window, box []string, width int, colored bool) []string {
	return overlayBoxRight(window, box, width, colored, max(len(window)-len(box), 0))
}
