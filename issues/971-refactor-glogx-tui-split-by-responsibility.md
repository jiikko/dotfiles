# 971 (refactor): glogx の tui.go (4241 行) を責務ごとに分割・解体する

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-29〜）

起票日: 2026-10-29

## 概要

ユーザー依頼 (2026-10-29「src/glogx/tui.go の責務ごとへの分割・解体」)。`tui.go` は 4241 行・関数 145 個で、直近 30 日の commit が 63 本と
src で最多。その中には treefiler・mac-lan-manager の機能追加の commit も混ざる (別の画面の変更も tui.go を触る)。
`browseModel.update` は 1 本の type switch で 659 行あり、43 種類のメッセージの処理を直接持つ。

## 何が複雑か (分割で良くなるもの)

`src/glogx/CLAUDE.md` は「行数や責務の見た目で割らない」とし、`structure_ratchet_test.go` も行数の上限を採っていない。
そこで、ファイルを分けるだけ (複雑性の移動) ではなく、次の 2 つが実際に良くなるかで分け方を決める
(`~/.claude/rules/verify-design-intent-before-refactor.md` の 3 問)。

1. **update の switch に処理が直書きされている**: CI のポーリングの結果・job の詳細・push / pull の結果・エディタを閉じた後など、
   責務の違う処理が 1 つの関数の中に並ぶ。ある責務を直すと、関係ない 42 種類の case と同じ関数の差分になる。
   → 2 文以上の case の本体を、責務ごとのファイルの `on<Msg>` メソッドへ切り出す。update は「どのメッセージを誰が受けるか」の
   表だけになる (1 文の case は既に 1 行の委譲なのでそのまま残す)。
2. **同じ責務の関数が tui.go の中で散っている**: たとえば CI の取得は 632〜730 行 (取得) と 2983〜3155 行 (ポーリング・ETA) に分かれ、
   その結果を受ける case は update の中にある。→ 責務ごとに、その関数・型・定数・`on<Msg>` を 1 つのファイルに集める。

browseModel の状態 (フィールド) は分けない。分けるのは「どの関数がどこにあるか」と update の中身だけで、状態の持ち主は 1 つのまま
(状態を分けると、同じ排他で守っていた状態が別々の単位へ割れる形になりうる。今回の範囲の外)。

## 分け方 (package main のまま。全部 browseModel のメソッド)

| ファイル | 責務 | 主な中身 |
|---|---|---|
| `tui.go` | 型・起動・配線の表 | `browseModel` の型、`newBrowseModel`・`Init`・起動時の復元、`Update` / `update` (メッセージ → 受け手の表)、`RunBrowse` |
| `browse_keys.go` | キーの入口と振り分け | `handleKey`・`updateKeyReachable`・`applyListMotion`・キーリピートの抑止・入力欄の編集キー、`onKeyPress`・`onPaste` |
| `browse_screens.go` | 全画面へのキーと結果の受け渡し | `routeKeyTo{Doctor,RatelimitDash,Issues,Status,Filer,Lan}`・`restore{Filer,Lan}Screen`、各画面の非同期の結果の `on<Msg>` |
| `browse_ci.go` | CI 状態の取得・ポーリングと job パネル (同じ状態を同じ順に触るので 1 ファイル。設計レビューの P2) | `fetchCIStatusesCmd`・`startCIFetch`・`ciPoll*`・`settleAwaitCI`・`openPanel`・`handlePanelKey`・job 詳細・rerun・ETA・job ログ、対応する `on<Msg>` と型 |
| `browse_overlays.go` | コミットに重ねる diff / PR / PR status と URL | `openDiff`・`handleDiffKey`・`openPR`・`openPRStatus`・URL のコピーと open、対応する `on<Msg>` と型 |
| `browse_log.go` | git log の読み直しと push / pull | `reloadLog`・`applyLogData`・push / pull とその演出 (`start/advance{Pull,Push}Anim`・`slideColumns`)・`refetchAfterPush`、`onPull`・`onPush` と型 |
| `browse_lifecycle.go` | 終了・再起動・更新の知らせ | `cancelAll`・`handleQuitKey`・`quitWith`・autobuild の再起動の確認・claude / codex の更新、対応する `on<Msg>` と型 |
| `browse_anim.go` | 時間で進む演出と知らせ | `tickEvery`・`maybeTick`・`tickInterval`・`onTick`、`showWarning`・`showGitFailure`・`deliverNotice`・`onToast` |
| `browse_usage.go` | 利用枠の表示 | `toggleUsage`・`toggleRatelimitDash`・`usageRefreshTick`、`onUsageRefresh` と型 |
| `browse_view.go` | 描画 | `View`・`viewLines`・`finishWindow`・案内の行・幅と頁・カーソル・背景・spinner・各画面へ渡す描画の options |

- 既存の `gitlog_watch.go`・`fullscreen.go`・`hint_surfaces.go`・`open_workspace.go`・`action_modal.go` はそのまま (既に責務ごとに分かれている)。
- 新しい 9 ファイルは `structure_ratchet_test.go` の `mainSourceFiles` と `browseModelMethodFiles` に理由つきで足す (検査の目的は
  「新しい画面が browseModel にメソッドを生やす」形を止めることで、今回は既存の状態機械の分割なので一覧の更新が正当な形)。
- `src/glogx/CLAUDE.md` の「ファイルの地図」を同じ変更で直す。

## 挙動を変えないことの担保

- 移すのは宣言の位置だけ。関数の本体・コメントは書き換えない (case の本体は、1 段字下げを戻し、本体の最後が return で無いときだけ
  `return m, nil` を足す)。update の switch に `break`・`fallthrough`・`goto` が無いこと、case の本体が `m` と `msg` 以外の外の変数を
  使っていないことを確かめた (switch の後は `return m, nil` だけ)。
- 手で写さず、`go/parser` で位置を取ってテキストを切り貼りする使い捨ての道具で行う (写し間違いを作らない)。
- commit を 2 つに分ける: ① update の case を `on<Msg>` へ切り出す (同じ tui.go の中) ② 宣言を責務のファイルへ移す。
  どちらもテストを変えずに `make -C src/glogx test lint` が通ることを等価の証拠にする (変えるのは構造の検査の一覧だけ)。
- glogx は Bench の対象 (`.claude/rules/bench-watch-after-push.md`)。push 後に Bench を見る。

## 関連ファイル

- `src/glogx/tui.go`・`src/glogx/structure_ratchet_test.go`・`src/glogx/CLAUDE.md`

## 設計のレビューの決着 (opus、読み取りのみ)

update の case を切り出して挙動が変わる case は見つからなかった (break / fallthrough / goto・名前付きの戻り値・defer・複数の型の case が無い。
update の前段は update に残る)。反映した指摘:
- P1 同じ分割は tui.go の browseModel の直前のコメントで「tui.go に触らずに済む commit は 2〜3 割」として見送られていた (再提起には同じ方法で
  数え直すことを求めていた)。数え直した: 直近 90 日に tui.go を触った 181 commit (`git log -p -U0` の hunk の関数見出し) のうち、分けた後に
  tui.go へ触らずに済むのは **39% (71 本。update の hunk を tui.go に残ると数えた下限) 〜 67% (122 本。on<Msg> へ出た case の中の変更も
  外へ出ると数えた上限)**。残るのはフィールド (30)・Init (26)・newBrowseModel (16) が多い。前の 2〜3 割は update の case を出さない案のもの。
  コメントをこの数字と「状態を割らない理由」に書き換えた。ユーザー依頼が明示の分離要望なので進めた
- P1 forbidigo の「状態機械の時刻は app.TimeNow」は `path-except: tui\.go` で tui.go にしか掛かっていなかった → `(tui|browse_[a-z]+)\.go`
- P1 「テスト不変が等価の証拠」は成り立たない: ファイル名を名指しするテスト 3 本 (fullscreen_test・hint_registry_gate_test・
  waitdelay_discipline_test) を直す必要がある → 直した (直す前はどれも黙らずに red)。等価の証拠は行の突き合わせに替えた (下)
- P2 `.github/workflows/doctor.yml` の paths が tui.go だけ → `src/glogx/browse_*.go` を足した
- P2 KeyPressMsg の受け方のコメントが PasteMsg の上にずれていた → KeyPressMsg の case の上へ戻した
- P2 CI の取得と job パネルは同じ状態を同じ順に触る → 表の browse_ci.go と browse_jobs.go を 1 つ (browse_ci.go) にまとめた
- P2 表に無い宣言の行き先 → 対応表 (名前 → ファイル) で全部決めた。tick の間隔と maxPanelJobs の const ブロックは tui.go に残した
- P3 代入 1 つと return だけの case は薄い委譲になる → 2 文以下の case は update に残した (切り出したのは 3 文以上の 24 個)
- P3 tui.go を名指しするコメント・文書 → 移した先に直した (issues/done の過去の記録は直さない)

受けた (直していない): gitlog_watch.go の `handleGitLog*(msg) tea.Cmd` と新しい `on<Msg>(msg) (tea.Model, tea.Cmd)` で名前の付け方が 2 つになる
(戻り値の形が違うので揃えない)。

## 進捗

- [x] 設計のレビュー (上)
- [x] ① update の case の切り出し — 「refactor(glogx): update の case の本体を on<Msg> メソッドへ切り出す (issue 971 ①)」。update は 659 → 148 行。
  テストは変えずに `make -C src/glogx test lint` が緑
- [x] ② 責務ごとのファイルへの移設 — 「refactor(glogx): browseModel のメソッドを責務ごとの browse_*.go へ移す (issue 971 ②)」。
  tui.go 4241 → 554 行、browse_*.go 9 本 (ci 1024 / view 508 / log 469 / keys 457 / screens 435 / overlays 360 / lifecycle 332 / anim 253 / usage 69)
  - 等価: 移す前の tui.go と移した後の tui.go + browse_*.go の行を並べ替えて突き合わせ、差は import の行と書き換えたコメントだけ
  - 変異: browse_anim.go に `var _ = time.Now` を足すと make lint が forbidigo で red (lint の範囲の広げ方が効く)
  - dupscan gate rc=0 (変わった単位 187、写しの増えなし)
- [x] 実装の敵対的レビュー (opus、読み取りのみ): ① ② とも挙動は壊せず、外れた検査も見つからなかった。
  - ① の on<Msg> 24 本の本体は、字下げを戻すと旧 update の中に全部見つかる (24/24)。旧の case はどれも return で終わっていて、
    ① で足した `return m, nil` は 0 件 (① の commit message の「本体の最後が return で無い case にだけ足した」に当たる case は無かった。訂正)
  - ② の新しいファイルの区間は全部、旧の宣言の境目から始まり、重なり 0・取りこぼしは import と書き換えたコメントだけ。
    旧の tui.go に `//go:`・build tag・`//nolint` は 0 件
  - 反映した P3 4 件 (「docs(glogx): 移設の後のコメントの指す先を実体に合わせる」): forbidigo の path-except を `browse_[a-z0-9_]+` へ
    (`browse_ci_panel.go` のような名前で黙って外れる)・bubbletea-v2 の表の RunBrowse・term/box.go の panelLines・ratchet の canary のコメント。
    前からずれていた 2 件 (lanmgr の normalizeSpaceKey・frame_alloc_test の usageOverlay) も直した
  - 直した差分はコメントと正規表現だけなので、2 周目の敵対的レビューは省いた
- [x] ルートの make test: 1 回目は test-comment-provenance (足したコメントの issue 番号) と test-yaml (120 字) が赤 →
  「docs(glogx): 分割で足したコメントから経緯の語を外し、yaml の行の長さを戻す」で直した。2 回目の赤は `tests/claude/test_dangling_symlinks.sh` だけ
  (`~/.claude/agents/` に dotfiles 側で消えた agent への link が残っている。この変更と無関係で、前から赤)
- [x] push・CI・Bench (2026-10-29): 私の commit を含む最新の commit で Tests (run 40882469253。push が続いて取り消されたので再実行) / Lint / unused /
  src/glogx / src/doctor / doctor (paths に browse_*.go を足したレーン) が緑。Bench は run 40881558987 (c35f66bca、trusted=yes) で緑。
  前の緑の Bench (run 40877654164、242261f64、変更の前、trusted=yes) と glogx の metric を比べた: 確保量 (alloc_kb) は全 metric で同じ、
  時間は x0.78〜x1.02 (view_panel 0.045 → 0.046、status_view_scale_x100 78 → 79 が x1.01〜1.02、他は速い側。どれも run 間のばらつきの範囲)。
  コードは移しただけなので、時間が変わる機構は無い
