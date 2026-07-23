# 025 feat(glogx): 画面全体を「浮かぶ板」として描く最外周フレーム + ドロップシャドウ

## 背景

glogx は TUI なので OS ウィンドウ (ターミナルエミュレータのウィンドウ) の外にシャドウを
描くことはできない。代わりに、**最外周に 1 セルの余白を残して枠を内側にレンダリングし、
余白側へ右下ドロップシャドウを落とす**ことで「glogx の板がターミナル地色の上に浮いている」
見た目を作る (ユーザー要望 2026-07-23)。

### 前例との関係 (4fb36a2 revert)

popup 枠 (job/diff/push) への全面シャドウは一度導入 → 「面積が大きく影が主張しすぎる」で
revert した経緯がある (4fb36a2、box.go `buildShadowPanelBox` の docstring に記録済み)。
本 issue はそれと衝突しない判断:

- revert された影は**リストのテキストに重なって浮く**大面積ボックスの影で、視覚ノイズが
  コンテンツを汚した。今回の影は**画面端の余白セル**にだけ落ち、コンテンツと重ならない
- revert 当時 (d25ce07) の影は 256 色 233 番の **bg ベタ塗りの硬い黒矩形** (NO_COLOR は ░)
  1 種類だった。現在は前景ブロック █ + ▓ フェザー + 細罫線下辺 ▖▁▗ (3dd73fd〜、2026-07-23) に
  洗練され、action モーダル / tmux prefix トースト / usage / toast の影として定着している
- とはいえ大面積の視覚判断は実機目視が最終決定。フラグ (`--frame` / `--no-frame`) の脱出
  ハッチを最初から用意し、既定の反転・revert を 1 行で済むようにしておく。**既定を ON/OFF
  どちらにするかは主要環境 (tmux popup) 次第で未決** — 下記「有効化の seam」の判断待ち

着地時に box.go のシャドウ関連 docstring を改訂すること (コメントと実装の乖離を作らない)。
`buildShadowPanelBox` の docstring 第 1 文「confirm モーダル (push / pull --rebase) 専用」は
既に stale で、実際の呼び出し元は 4 系統 (centerBox 経由の action モーダル + tmux prefix
トースト / toast / usage)。末尾一文を足すだけでは「専用」の嘘が残るため docstring 全体を
書き直す。加えて影の**適用方針**コメント (「大面積 popup 禁止 4fb36a2 / 小面積モーダルと
最外周枠のみ」) は、wrapWindowFrame が `buildPanelBoxImpl(shadow=true)` を直接呼ぶ設計上、
`buildPanelBoxImpl` 側か module 冒頭 charter に置く方が整合的。

## 見た目 (mock)

```
                                            ← 上余白 1 行 (端末地色)
 ┌────────────────────────────────────┐     ← 左余白 1 桁 + dim 枠
 │ ● a1b2c3d feat: subject...         │▓   ← 右影の最上段はフェザー ▓
 │     Author: koji                   │█   ← 以降は本体 █
 │ ○ e4f5a6b fix: subject...          │█
 │   (リスト/パネル/モーダルは全部この中) │█
 ▖▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▗█   ← 細罫線下辺 + 右下角 █
   ▓██████████████████████████████████    ← 下影 (offset 2 + フェザー + 本体)
 j/k: 移動  Enter: CI job  ...             ← hint は板の外・最下行 (タスクバー的)
```

影のグリフ・色・下辺の意匠は confirm モーダル (`buildShadowPanelBox`) と完全に同一
(NO_COLOR 時は ▒/░)。画面内に「同じ光源の影」が 2 種類あると嘘になるため、新しい影の
描き方は発明しない。

## 設計

### 原則: 合成の最終段で 1 回だけ包む

`View()` は今まで通り「コンテンツ幅 × ページ行数」の `window []string` を組み上げ
(リスト + overlay 群 + アニメ)、**最後に純関数 1 つで板に包む**。内側の描画コードは
「自分がフレームの中にいる」ことを知らない。

```go
// box.go (純レイアウト関数の置き場。box.go 冒頭の charter コメントに適合)
// wrapWindowFrame は画面全体のコンテンツを余白 + 枠 + 右下ドロップシャドウで包む。
// 影の幾何は buildPanelBoxImpl(shadow=true) へ完全委譲する (影の実装は 1 箇所)。
func wrapWindowFrame(content []string, termW int, colored bool) []string {
    box := buildPanelBoxImpl("", content, termW-2, colored, true) // -2 = 左右余白
    out := make([]string, 0, len(box)+1)
    out = append(out, "") // 上余白
    for _, l := range box {
        out = append(out, " "+l) // 左余白 1 桁
    }
    return out
}
```

`buildPanelBoxImpl` は既に「ANSI 実効幅での右パディング + clip + 右影フェザー +
細罫線下辺 + 下影」を全部持っている。full-screen 版を別実装すると影の幾何が二重管理に
なるため、**委譲一択**。

### 寸法の一本化: contentWidth() を新設、pageSize() を改修

高さは既に `pageSize()` が単一ファネル (scroll 半ページ / pull アニメ / detail 行数 /
diff 行数 / clampOffset / ensureCursorVisible / View の全てが経由) なので、フレーム分を
引くだけで全消費者へ自動伝播する。幅にはファネルが無いので新設する:

**フラグ名の注意**: `browseModel` には既に `frame int` (spinner アニメカウンタ。
`m.frame++` / `spinnerFrames[m.frame%…]`) がある。新フラグを `frame` にすると型衝突で
コンパイル不能なので **`windowFrame bool`** とする (spinner の `frame int` は無関係・不変)。

```go
// フレーム有効時の寸法オーバーヘッド。
// 横 7 = 左余白1 + "│ "2 + " │"2 + 影1 + 右余白1
// 縦 5 = 上余白1 + 上辺1 + 下辺1 + 下影1 + hint1
const (
    frameHOverhead  = 7
    frameVOverhead  = 5
    frameMinWidth   = 60 // これ未満はフレーム自動 OFF (従来描画)
    frameMinHeight  = 15
)

func (m *browseModel) frameActive() bool {
    return m.windowFrame && m.width >= frameMinWidth && m.height >= frameMinHeight
}

func (m *browseModel) contentWidth() int {
    if !m.frameActive() {
        return max(m.width, 1) // 下限クランプ (下記フォールバック一本化の前提)
    }
    return m.width - frameHOverhead // frameActive は width>=60 を要求 = 常に正
}

func (m *browseModel) pageSize() int {
    if !m.frameActive() {
        return max(m.height-1, 1)
    }
    return max(m.height-frameVOverhead, 1)
}
```

`m.width` を直接読む描画箇所を全て `contentWidth()` 経由に置換する。以後
「`m.width`/`m.height` を読んでよいのは wrapWindowFrame 呼び出しと contentWidth/pageSize/
frameActive だけ」を規約とする (置換対象の全数は下の実装詳細)。

不変条件 2 つ:

- **`m.windowFrame` は起動時固定** (キーによる実行中トグルは無い)。`frameActive()` は
  resize で `frameMinWidth`/`frameMinHeight` をまたぐ時だけ ON/OFF が変わる。その入力
  (`m.windowFrame` + `m.width`/`m.height`) は **WindowSizeMsg でのみ変化**し、そこでは既に
  `invalidateLines()` を呼んでいるので、`linesCache` の invalidation ポイントは増やさなくてよい
- **`width<=0 → 80` フォールバック 4 箇所の削除**: 現状このフォールバックが `centerBox` /
  `panelLines` / `diffOverlay.boxLines` / `prStatusOverlay.boxLines` に重複している。これらは
  **起動後 (WindowSizeMsg 到達済み = width>0) の popup 描画でしか呼ばれない**ため、実行時に
  `width<=0` に到達しない死んだガード。加えて frame 経路の `contentWidth()` は
  `frameActive()` が `width>=frameMinWidth(60)` を要求するので常に正 (負値流入は起こらない)。
  よって 4 箇所は削除して `contentWidth()` 側の `max(m.width,1)` に一本化できる (CLAUDE.md
  自律改善: 重複・死にガード除去)。**注意**: `contentWidth()` 自体をクランプする一方で、
  置換後の `renderOpts().Width` 等の `- cursorGutterWidth` は従来どおり `max(…, 0)` で包む
  (幅 1〜2 の極小端末で負値が RenderLines に届かないよう既存ガードを維持)

### 板の高さを安定させる — パディングは overlay 合成より「前」

コンテンツが少ない時に板が縮むとリサイズのたびに枠が踊るので、`window` を `pageSize()` 行まで
空行 `""` でパディングして常にビューポート一杯にする。`buildPanelBoxImpl` が空行も inner 幅まで
パディングするので追加コストなし。

**順序が重要**: パディングは **overlay 群 (panel/diff/prStatus/centerModal/usage/toast) を
合成する前**、コミット行を切り出した直後に行う。右下トースト (`overlayBoxBottomRight`) と
中央モーダル (`overlayCenteredBox`) は `len(window)` を基準に位置決めするため、合成の後に
パディングすると **window が page より短い場合にトーストが板の途中に浮き、その下に空行が
並ぶ** (少コミット repo + 起動時トースト = macism 未導入警告 / claude update 通知で現実に
踏む)。padding-first なら全 overlay が page 長の window 上で合成され、トーストは板の下辺
直上に着地する。これは frame 無効時の従来経路には影響しない (frame 有効時のみ pad)。

### hint 行は板の外

hint は最下行に今まで通り置く (板の下・影のさらに下)。理由:

- hint はメタ UI (タスクバー的存在)。板 = ログコンテンツ、という区別が立つ
- 板の中に入れると縦オーバーヘッドが +2 (仕切り or 下辺との衝突回避) になる
- 目線の習慣 (画面最下端にヒント) を変えない

左余白 1 桁を hint にも付けて板の左端と縦に揃える。

### 有効化の seam: Options (既定は未決 — ⚠ 下記「既定の決定」参照)

- フラグ `--no-frame` (または `--frame`) で ON/OFF (`options.go` の `ParseArgs` + `Usage()` に追加)
- `newBrowseModel` で `m.windowFrame = <既定> ^ フラグ` を設定
- 極小端末 (`frameMinWidth`/`frameMinHeight` 未満) では `frameActive()` が自動で false に
  なり、**従来と完全に同一の描画**へフォールバック (tmux の小ペイン・popup でも安全)
- 初回 `WindowSizeMsg` 前 (`m.width == 0`) も同じ条件で自動 OFF

#### ⚠ 既定の決定はユーザー判断 (この issue で最も重要な未決事項)

**glogx の主要実行環境は tmux `display-popup` 内**で、display-popup は既定で自前の枠を描く。
この前提でフレームの既定を決めると:

- **popup が枠ありのまま既定 ON** → 初回起動で「popup の枠 + glogx の枠」の**二重枠**。しかも
  この feature は本来「glogx の窓がターミナル地色に浮く」ものだが、popup 内では「popup の中に
  内側の板がある」別物になり、ユーザーの元々の要望 (窓自体に影) とはズレる
- 素のターミナル直起動・または `display-popup -B` (枠なし) で起動 → 既定 ON がちょうど嵌まり、
  glogx の枠が窓の縁として機能する

つまり既定の正解は**ユーザーが glogx をどう起動しているか (popup の枠有無)** と**見た目の好み**
に依存し、コードだけでは決められない。取りうる立場:

1. **既定 OFF + `--frame` で opt-in** — 二重枠事故をゼロにする最も安全な既定
2. **既定 ON + `--no-frame`** — 素のターミナル / 枠なし popup 前提。popup 枠ありだと二重枠
3. **popup 検出で自動 OFF** — `$TMUX` や環境から popup 内かを推定。ただし確実な検出は難しく
   過剰実装になりうる (まずは手動フラグで足りるか判断)
4. **popup 内は枠線を省き影だけ落とす** — 二重枠は避けつつ浮遊感は残す折衷 (実装は増える)

→ **着手前にユーザーへ確認する**。glogx の起動方法 (popup の枠設定) と、素の見本 vs popup 内
見本の両方を見た上での好みで決める (視覚見本を添えると即決されやすい)。

## 実装詳細 (touch points)

### tui.go — m.width 直読みの contentWidth() 置換 (全数)

| 箇所 (シンボル) | 現状 | 変更 |
|---|---|---|
| `View()` リスト行 clip ×2 | `m.width - cursorGutterWidth` | `contentWidth() - cursorGutterWidth` |
| `View()` → `overlayCenteredBox` | `m.width` | `contentWidth()` (板の中で中央寄せ) |
| `View()` → `usageOv.boxLines` / `overlayBoxTopRight` | `m.width` | `contentWidth()` (板の右上角に寄る) |
| `View()` → `toast.boxLines` 側 `overlayBoxBottomRight` | `m.width` | `contentWidth()` |
| `panelLines()` | `width := m.width` | `contentWidth()` |
| `diffBoxLines()` → `diffOv.boxLines` | `m.width` | `contentWidth()` |
| `centerModalLines()` (tmux 警告 `centerBox` / `actModal.boxLines`) | `m.width` | `contentWidth()` |
| `prStatusOv.boxLines` | `m.width` | `contentWidth()` |
| `cursorLine()` の clip | `m.width` | `contentWidth()` |
| **`bgLine()`** の clip + 空白 pad (カーソル行の bg を端末幅いっぱいに敷く) | `m.width` | `contentWidth()` — **置換漏れ筆頭**。漏れると bg 塗りが枠線・右余白まで食い込み最も目立つ |
| `hintLine()` の clip | `clipToWidth(hint, m.width)` | **単純に左余白を前置してはいけない**。既定 hint は表示幅 100 超で、60〜100 桁端末では clip 後が常に `m.width` ちょうどになる。そこへ `" "` を足すと実効幅 `m.width+1` で折り返しレイアウトが崩れる。frameActive 時のみ `" " + clipToWidth(hint, contentWidth()+…)` 相当で**左余白分を clip 幅から差し引く** (板の左端 ┌ と縦に揃える)。canary テストが即座に落とす |
| `renderOpts()` の `Width` | `m.width - cursorGutterWidth` | `contentWidth() - cursorGutterWidth` |
| `slideColumns()` の `depth` | `m.width / 2` | `contentWidth() / 2` (push 演出の沈み込み距離) |
| `pageSize()` | `m.height - 1` | 上記の通り frame 分岐 |

`pageSize()` 経由の消費者 (scroll 半ページ・pull アニメの行数判定・`visibleDetailRows`・
`visibleDiffRows`・`clampOffset`・`ensureCursorVisible`) は無変更で追随する。

**docstring の乖離掃除** (touch したら直す): `overlayBoxBottomRight` の docstring
「window の下端 = hint 行の直上」はフレーム有効時に stale になる (toast の下端は板の下辺の
直上、hint はさらに下辺 + 下影の 2 行下)。ここも同じ PR で「window 下端 = フレーム時は板の
下辺の直上」へ直す。

### View() の 2 段構え (padding-first)

```go
// (1) コミット行を offset..end で切り出した直後、overlay 合成の前にパディング。
//     overlayBoxBottomRight / overlayCenteredBox が len(window) を基準にするため。
if m.frameActive() {
    for len(window) < page {
        window = append(window, "")
    }
}

// … 従来どおり overlay 群を合成 (panel/diff/prStatus/centerModal/usage/toast) …

// (2) 最終段は wrap だけ。合成済み window を板に包む。
if m.frameActive() {
    window = wrapWindowFrame(window, m.width, m.colored)
}
// 以降は今まで通り window を join + hintLine()
```

### box.go

- `wrapWindowFrame` 追加 (上記)。タイトルは空文字 (`┌──…──┐`)。将来 repo 名等を入れたく
  なったら title 引数を通すだけ (スコープ外)
- シャドウ関連 docstring の改訂 (背景節「前例との関係」末尾に詳細): `buildShadowPanelBox`
  の stale な「confirm モーダル専用」を実際の呼び出し元 4 系統込みで書き直し、影の適用方針
  コメント (小面積モーダル + 最外周枠のみ / 大面積 popup 禁止 4fb36a2) を
  `buildPanelBoxImpl` 側か module charter に置く

### options.go

手書き allowlist 方式なので追加箇所は 4+1: `Options` フィールド / `ParseArgs` の case /
`usageShort()` の対応引数リスト (未記載だと UnsupportedArgError のメッセージから漏れる) /
`Usage()` のオプション節 / `options_test.go` の `TestParseArgsFlags` (bool フラグ表テスト)。
環境変数 (GLOGX_NO_FRAME 等) は足さない — フラグで足りる。`ParseArgs` は env を読まない
純関数を維持する

### main.go / tui.go 接続

- `newBrowseModel` の構造体リテラルに `windowFrame: !opts.NoFrame` を追加
  (シグネチャ変更なし。opts は既に渡っている)

## テスト

- **既存テストは無傷にする**: `tui_helpers_test.go` の `newTestBrowse` (全描画テストの
  単一ファネル) が `&Options{}` を渡している箇所を `&Options{NoFrame: true}` に変える
  1 行で、既存の View/overlay/panel テストの期待値は一切変わらない
  (現行 80×10 は frameMinHeight 未満で自動 OFF だが、途中で `m.width`/`m.height` を
  大きくするテストが frame を踏まないよう明示 OFF が決定的)
- `wrapWindowFrame` の純関数テスト (box_test 系):
  - 出力行数 = len(content) + 4 (上余白 + 上辺 + 下辺 + 下影)
  - 全行の ANSI 実効幅が termW 以内 / 枠行は左余白 1 + ┌…┐
  - 右影列: 最上コンテンツ行 ▓、以降 █、右下角 █、下影行 = offset 2 + ▓ + █run
  - NO_COLOR (colored=false): ▒/░ 系グリフ
- frame ON の統合テスト (新規、明示的に `windowFrame=true` + 60×15 以上で構築):
  - `View()` の行数 = m.height (hint 込み) ちょうど
  - コンテンツが少なくても板が pageSize まで伸びる (空行パディング)
  - `--no-frame` / 極小端末で従来描画と byte 一致
  - overlay (usage 右上 / toast 右下 / 中央モーダル) が枠の内側に収まる
  - **幅不変条件の canary**: 既存 `TestBrowseWrapUsesFullWidth` /
    `TestJapaneseFullViewStaysInWidth` (全行の実効幅 <= m.width を assert) と同型の検証を
    フレーム ON でも流す。contentWidth() の置換漏れはこれが最短で検出する
- `ParseArgs` の `--no-frame` 表テスト (`TestParseArgsFlags` に 1 行)
- 検証コマンド: `make -C src/glogx lint && make -C src/glogx test` (CI の src_glogx.yml と
  同等。root からは `make test-src`)

## リスクと判断根拠

- **【最重要】tmux display-popup 内の二重枠 → 既定の決定はユーザー判断**: 詳細と選択肢は
  上の「有効化の seam / ⚠ 既定の決定」を参照。この feature の成否を最も左右する未決事項で、
  着手前にユーザー確認が要る (コードだけでは決められない)
- **視覚評価が最終関門**: 4fb36a2 の前例がある通り、大面積の影の良し悪しは実機目視で
  決まる。実装後にスクリーンショット/実機確認 → 気に入らなければ既定の反転 (1 行) or revert。
  脱出ハッチ (フラグ) を先に作るのはこのため
- 表示領域が横 7 桁・縦 4 行減る。subject は `subjectWidthCap = 60` で既に上限があるため
  実害は小さい。狭い端末は自動 OFF で守る
- 右影 1 桁 + 下影 1 行は**端末地色の上に置く前景ブロック**なので、ユーザーのターミナル
  背景色 (テーマ) との調和は環境依存。confirm モーダルの影で既に許容済みの特性
- **トーストの右スライド演出の見え方が変わる**: 現在は「画面右外から滑り込む」錯覚を
  overlayBoxBottomRight に渡す width (= クリップ右端) で作っている。フレーム有効時は
  右端が板の内側になるため「枠線の内側から生えてくる」見た目になる。あふれは
  contentWidth() クリップで構造的に防がれるので、演出の解釈が変わるだけ (実機目視で許容
  判断。気になるなら将来の調整項目)
- 低い端末での overlayBox は page を超えた box 行を黙って捨てる仕様のため、pageSize が
  4 行縮むと job パネル + 詳細の見切れが早まる。`frameMinHeight` の自動 OFF が防波堤
  (15 は「pageSize 10 = job パネル + 詳細最小 3 行が収まる」を目安に実装時に微調整可)

## トリガーと優先度

**着手可能** (トリガー充足)。着手を待っていた issue 024 (claude update トースト) は
33d85d5 で着地し done へ移動済み (a3308b4)。box.go は直近 1 日で活発に触られている
(707fb2f → 5213ad8 → d8ceef6 → 364db00 → 2ca05a1 → ba90d33) ので、着手直前に
`git log --oneline -- src/glogx/box.go` で影の最新意匠を再確認すること。tui.go を触る
並行セッションが居たら worktree 退避 + pathspec commit の規律に従う。

## スコープ外

- 上辺タイトル (repo 名 / ブランチ名) の表示 — title 引数を通すだけの拡張として将来判断
- 板の背景色ベタ塗り (bg fill) — コンテンツ行の ANSI と衝突し複雑化する割に、枠 + 影で
  浮遊感は十分出る (confirm モーダルと同じ判断)
- インラインモード (`RenderStatic` / 非 AltScreen 経路) への適用 — フレームは
  browse TUI 専用。`fitsTerminal` (「収まるなら TUI に入らず静的出力」の less -F 相当判定)
  も raw の端末高のまま変えない (フレーム分を織り込むと静的経路が選ばれやすくなる方向へ
  数行ずれるが、静的出力にフレームは無いので織り込む方が誤り)
- 影の色・濃度のテーマ対応
