# 023 refactor(glogx): tui.go の枠描画ヘルパー分離と tui_test.go の機能クラスタ分割

## 背景 (2026-07-23 の会話由来)

tui.go は 2,164 行 / 77 関数まで成長した。ただし issue [018](done/../018-refactor-god-struct-audit-2026-07-22.md)
の God struct 監査で「状態を持つ UI 部品 (usageOverlay / diffOverlay / actionModal /
jobDetailOverlay、後に prStatusOverlay も) は抽出済み。browseModel 本体に新規抽出の価値なし」と
判定済みであり、残る本体 (Update ディスパッチ / fetch オーケストレーション / アニメーション) は
状態機械の不可分な写像なので**分割しない** (分割は複雑性の移動にしかならない)。

その上で残っている分離余地は 2 つ:

1. **tui.go 内の純粋な枠描画ヘルパー** (~200 行) — browseModel の状態を一切参照しない純関数群。
   状態機械と描画プリミティブの分離として唯一きれいな継ぎ目
2. **tui_test.go (2,700 行超)** — こちらの方が読みにくさの兆候が先に出ている。テストは機能
   クラスタでファイル分割しても挙動リスクゼロで、実益は 1 より大きい

## やること

### 1. tui.go → box.go (枠描画プリミティブの分離)

対象 (いずれも browseModel 非依存の純関数):

- `buildPanelBox` / `buildShadowPanelBox` / `buildPanelBoxImpl` / `shadowRun`
- `overlayBox` / `overlayCenteredBox`
- `centerBox`
- `cursorGutterMark` / `cursorGutterBlank` / `cursorGutterWidth` / `cursorMark`

機械的な移動のみで挙動変更なし。`m.cursorLine` / `m.bgLine` は m.width / m.colored に
依存するので tui.go に残す。

### 2. tui_test.go → 機能クラスタで分割

分割案 (テスト関数は無改変で移動のみ。ヘルパー `newTestBrowse` / `statusesFor` / `withJobs` /
`withFailedJob` / `deliverMsgs` は共有されるため `tui_test_helpers_test.go` などへ):

- `tui_nav_test.go` — カーソル移動 / スクロール / glide / pull アニメ / View 窓
- `tui_panel_test.go` — job パネル / job 詳細 / ETA / panelPoll (grace 含む)
- `tui_actions_test.go` — push / pull / rerun / claude update / actionModal / toast 連携
- `tui_overlay_test.go` — diff / usage / PR 状態 / prefix トースト / コピー系 (y/Y)

境界は着手時に実際のテスト関数リストを見て再調整してよい (上の 4 分類は目安)。

## トリガーと優先度

- 急がない。**trigger: 次に新しいオーバーレイ/モーダルを足すとき (→ 1 を同時に実施)、
  または次にテストを大きく追加するとき (→ 2 を同時に実施)**
- 単独作業として先行実施してもよい (両方とも移動のみでリスク極小、~15-30 分)

## 完了条件

- `make test` / `make lint` が green (テスト関数の増減ゼロ)
- tui.go に browseModel 非依存の枠描画関数が残っていない
- README「開発」節の境界説明に box.go を追記

## やらないこと (018 の再確認)

- browseModel 本体 (Update / handleKey / fetch オーケストレーション / アニメーション) の分割 —
  状態の所有権が曖昧になり複雑性の移動にしかならない。018 の判定を維持する
