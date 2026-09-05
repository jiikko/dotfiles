# research: glogx のテスト品質 / 機械化 監査 (2026-09-06)

起票日: 2026-09-06
カテゴリ: research

`/audit` を glogx に絞り、**test-cleanup / test-helpers / lint-from-done / general** の
4 観点で実行した記録。実行方式は forge (Minimum / Go ロスター 3 体) + main agent の検証。
**この issue は索引** — 起票したもの・却下したもの・攻めて外れた範囲を持つ。

前回（`issues/273`、2026-09-05、performance / resource-leaks）の範囲は除外指示として
エージェントへ渡し、再生成させていない。

## 全数勘定

| # | カテゴリ | 内容 | 変異で確定したか |
|---|---|---|---|
| 279 | bug | 基底一覧の hint が **174 桁**で既定幅では出口 `q: 終了` が消える（+ rlDash 70 桁 / prStatusOv 52 桁。264 の数え上げ漏れ 3 本） | main agent が実測 |
| 280 | test | フレーム確保予算が上界のみ。`View()` を空にすると **8 ケース全部が 0/0 で PASS** | main agent が独立に再現 |
| 281 | test | hint 幅ゲートが 201 の設計（走査型）でなく列挙表 / detailOv 節が恒真 | ✅（マーカーで変異の残存も確認） |
| 282 | test | 時計巻き戻しゲートが `IsZero()` を guard に数え、ガード無しの新規判定が PASS（走査 11→12 で緑） | ✅ probe |
| 283 | test | VS16 ゲートが `ParseDir` 非再帰で、サブパッケージが黙って対象外 | ✅（`usage/` に置くと PASS、`box.go` に置くと FAIL） |
| 284 | test | 無害化テスト 5 本に陽性対照が無く、箱が空になると vacuous | ✅（`return nil` の変異で PASS） |
| 285 | refactor | `hasTerminalControl` が 5 コピー（バイト一致・load-bearing コメント込み） | 静的事実 |
| 286 | chore | `~/.cache/glog` の `writeAtomic` 規約 — 219 の trigger の再評価 | 静的事実 + git 履歴 |
| 287 | test | `isDigitKey` の境界が無検査（`'0'`→`'/'` の変異で緑） | ✅ |

**母集合**: エージェント 3 体の生指摘（重複統合前 17 件）+ main agent の裏取り。
279 は 3 体のうち 2 体が別々の面（rlDash / 基底一覧）から指摘したものを 1 件に統合し、
main agent が実測で最重要（174 桁）を確定させた。

## 🚨 エージェント間で割れた論点と、その決着

**`~/.cache/glog` への素の `os.WriteFile`**（→ 286）:

- 2 体が「219 が確立した規約が破られている」として指摘
- 1 体が**却下**: 「素の `os.WriteFile` は復元失敗に安全に縮退するので意図的な規約。
  219 が問題にしたのは temp+rename の複製実装で、別物」
- **決着**（main agent が 219 本文を読んで判定）: 失敗モードの区別は却下側が**正しい**（本文に取り込んだ）。
  ただし 219 は「入れなかったもの」節で **trigger を明記**しており、`issues_state.go:71` を
  射程に挙げている。よって却下ではなく **trigger の再評価**として起票した

## 却下（理由つき。同じ指摘の再生成を止めるため）

- **却下: `TestHintsFitPopupWidth` の「doctor」サブテスト** — vacuous ではない。
  201 以前の姿（`fitHintItems` をやめて素の join）を当てると
  `doctorView.hint が 118 桁で幅 89 に収まらない` で **red**。サブテスト単位でも
  doctor だけが FAIL・他 2 つは PASS と確認した
- **却下: `doctor_docker_test.go:TestDoctorDockerVolumesHaveNoBulkCommand`** — absence-only に
  見えるが、第 2 の assert が `g.Command==""` 分岐の真の陽性対照になっており、
  bulk コマンドが出れば red。prune コマンドも既に**完全一致**で pin 済み
  （2026-09-04 の部分一致素通り事案は解消済み）
- **却下: `writeIssue` ×2 の重複** — シグネチャも役割も違う
- **却下: 既に機械化済みのもの** — `issues/done/216`（`test_no_real_commands_in_tests.sh`）/
  `219` の doctor 2 経路 / `222`（`src/doctor/.golangci.yml`）/ `251`（`check_go_project_lanes.sh` の
  replace_dirs + canary）/ `252`（`display_coverage_test.go`）。実物で確認して候補から落とした
- **却下: `issues/done/203` の lint-from-done との重複** — 203 は「glogx 以外が対象」と明記して
  おり、203 が却下した 4 候補とも重複しない

## 攻めて見つからなかった範囲（0 件と報告する）

- **assert を 1 つも持たないテスト関数**: 全テストファイル（サブパッケージ含む）を走査して
  実質 0 件（1 件は helper へ委譲する false positive）
- **常に skip されるテスト**: `go test -v ./... | grep '--- SKIP'` で **0 件**。
  `t.Skip` は 19 箇所あるが通常の checkout ではどれも発火しない
- **文書化された不変条件への変異 5 本はすべて killed**:
  番号フィルタ `Contains`→`HasPrefix`（RED ×4）/ `fitHintItems` の優先度ループ逆転（RED ×9）/
  `usage_cache.go` の Label 無害化を代入し忘れる形（RED）/ `usage/dial.go:versionTag` 同（RED）/
  `loadRatelimitScreen` の `!SavedAt.After(now)` 削除（RED）
- **健全なゲート**: `waitdelay_discipline_test.go` / `width_test.go:TestNoSecondWidthEngine` /
  `scripts/check_go_project_lanes.sh`（合成入力の canary + 実 go.mod を読めているかの錨まで持つ）
- **test-helpers の候補**: テストファイル全体の関数本体を正規化して md5 で突き合わせ、
  6 行以上の逐語重複は **1 組だけ**（→ 285）。`tui_helpers_test.go`（helper 20 本）が
  受け皿として機能しており、新規ヘルパーファイルは不要

## 🚨 プロセス上の事故（コード外。私の配線ミス）

**3 体全員に同じ worktree（`~/wt-audit2`）を渡してしまい、変異検証が互いに汚染された。**

- 1 体が「一度も編集していない `box.go` が `M` と出る」「当てた変異が `git checkout` なしで消えた」を観測
- 別の 1 体は自作の worktree（`wt-arch-audit`）にも他体が入り、`/tmp/mutbak.*` に
  自分が作っていない 5 ファイルが並ぶのを観測（**`/tmp` の固定名バックアップが衝突ベクタ**）
- 1 体が `git checkout --` で 3 ファイルを戻し、他体の in-flight な変異を消した可能性がある

**対応**: 途中で気づいて 3 体に専用 worktree を割り当て、
「共有期間の観測は破棄して取り直すか『未確定』と明記せよ」と伝達。
**本索引に載っている確定所見は、すべて専用 worktree での再取得**（各体が
①baseline green の確認 ②`go build` 成功の確認 ③`git diff` の目視 ④復元後の `git status` 空、
の手順を報告している）。

**教訓**（`parallel-write-agents-need-worktree-isolation.md` に既にある規範の実例）:
read-only のレビューは並行してよいが、**変異検証を伴う体は 1 体ごとに worktree を分ける**。
加えて**バックアップは `/tmp` の固定名でなくセッション固有ディレクトリに置く**
（worktree を分けても `/tmp` で衝突した）。

## 残課題

- [ ] 279（hint の生成契約を型で閉じる）/ 281（ゲートをレジストリ + 共通 sweep へ）
- [ ] 280（フレーム確保予算に下界）
- [ ] 282 / 283 / 284（ゲートが素通しする形の是正）
- [ ] 285 / 286 / 287
- [ ] `issues/done/201` の done 本文に「実装は列挙表になっている」を追記（→ 281）
- [ ] `fitHintItems` の doc「今の利用者は status viewer だけ。2 つ目が出た時点で共通の場所へ移す」が
      実態と乖離（実際は 5 呼び出し。`issues/done/242` が 2026-09-04 に「4 呼び出しで古い」と
      指摘してから 1 件増えた）。移動は推奨しないが、279 で doc を直すなら同じ commit で
