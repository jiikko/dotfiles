# 844 (research): glogx 全体監査 (2026-10-19) の全数勘定と、攻めたが見つからなかった範囲

起票日: 2026-10-19

## 概要

`/audit` の全 22 タイプを直接実行で `src/glogx` 全体に当てた (それまでの 663〜667・698・702・748〜754 は issues viewer が中心)。
調査体は 1 体ずつ直列に 6 波で回した (設計 7 タイプ / security・resource-leaks・broken-code・dead-code / error-handling・false-green・performance・concurrency /
test-cleanup・test-helpers / ux・general / dependency・ci・lint-from-done)。どの体も読み取りのみで、変異は `mutate-verify` の使い捨て worktree で当てた。
発見はすべて main agent が証拠ファイルかコードで照合してから採った。

## 母集合

- 非テストの .go: 99 ファイル・28,397 行 (24 package)。テスト: 161 ファイル・`func Test*` 1252 本
- 基準の `go test -count=1 ./...` は rc=0 (PASS 1239 + TestMain 13、SKIP 0)
- 既出の判定に使った issue: `grep -rl glogx issues/` で 378 件 (open / done)

## 全数勘定 (発見の行き先)

| 行き先 | 件数 | 中身 |
|---|---|---|
| 834 (P1) | 1 | git log の区切りの偽装 → `git show --output=` で上書き。同じ根の起動不能を含む |
| 835 (P2) | 1 | doctor の削除の Item 単位の不一致 |
| 836 (P2) | 1 | status viewer の X が確認後の変更も捨てる |
| 837 (P2) | 1 | 非 GitHub の remote で未 push が push 済みに見える |
| 838 (P2) | 4 | 失敗の理由が落ちる (push / pull・RunGitCmd・usage・status) |
| 839 (P2/P3) | 5 | 案内・文書と動作の食い違い (doctor の y・filer の入力中・周期・README・UI ガイド) |
| 840 (P3) | 10 | 品質系の小物 |
| 841 | 7 | 設計系 (今は正しいが増えたら壊れる構造) |
| 842 | 17 | 検査の素通り 7・境界の未検査 6・テストの重複と古い前提 4 |
| 843 | 8 | テスト helper の重複 |

タイプ別の 0 件: dead-code (staticcheck U1000 `-tests=false` は allowlist 済みの 1 件、`deadcode` の 19 件は 823 で受容済みの Fixture 系 16・`issues.BytesReadForTest`・allowlist 済みの `appZoom.start`・テストの失敗表示用の `FullScreenID.String`)、dependency (`go mod tidy -diff` 差分なし。charm 系・x/ 系の版は依存する module と一致。replace 10 本は実在)、ci (C1〜C3 は 0 件。C4 の test-budget の 10 秒は 842 の A3 に添えた)。`govulncheck` / `golangci-lint` は PATH に無く**未実行**。

## 攻めたが見つからなかった範囲 (次の監査の起点)

- 設計: 全画面の登録 (`fullscreen_id.go` / `CrossTarget` / `crosskey`。interface を採らない理由が実測付き)、`hint_surfaces.go`、commitview の overlay の同一性、`mergeCIBatch`、`applyLogData`、`app.ScreenMemory` の共有、repo / hasRepo の組 (11 か所ガード済み)、静的出力と TUI の計画の共有、view どうしの import
- security: issues の symlink の拒否 (`hasMarkdown` / `nextLinks` / scanDir)、`filelink.go` の repo の外への脱出、`rewriteIssue` のリンク拒否、statusview の pathspec (`:(top,literal)`)、`extcmd.ReadonlyCommand` の `--`、doctor の snapshot の信頼境界と brew の allowlist、CI のキャッシュのパス
- resource / concurrency: watcher の Close、timer の Stop、プロセスの起動 28 か所の WaitDelay と ctx、`app.CleanupLatch`、`WatchChain` / `drainWatchEvents`、`linecache`、ciPoll と awaitCI の札、`atomicfile.Write` の一時名、claim の同時実行 (symlink の作成が原子的)。CI のキャッシュの読んで混ぜて書くのは後勝ちで欠けうるが、取り直すだけなので害なしと判断
- false-green: `structure_ratchet_test.go` の H-1〜H-3・`hint_registry_gate_test.go`・`overlay_ownership_test.go`・`waitdelay_discipline_test.go`・`TestNoSecondWidthEngine`・`gorules/rules.go`
- ux: 一覧と全画面 6 枚を枠あり / なし・幅 1〜140・高さ 3〜40 (約 6.4 万回描画) で掃引し、溢れたのは幅 1 だけ。usage / R / job パネル (長い日本語 + ZWJ 絵文字) も溢れなし。幅の出典は `term.DispWidth` 1 本
- test-cleanup: assert を持たない Test 0 本、skip で走らない Test 0 本、同名 0 件
- lint-from-done: done の glogx 関連の bug 系 104 件を読み、既に検査になっているもの (WaitDelay・git の hardening・cache の書き込み・幅・時刻・termsafe・手書き列挙) を除くと、候補は環境 skip の可視化 (842 の A7) だけ

## 深く読んでいない範囲

- `doctor_delete.go` 以外の doctor の内部、mac-lan-manager の内部 (788)、issues viewer (既出の監査が厚い)
- 変異の約 20 本 (842 の「裏取りの範囲」)
- `FirstLine(` の 32 か所のうち push / pull 以外 (838)
- glogx の外で同じ区切りで git log を読むもの (pro-con など。834)
- `claude_version.go` の `http.DefaultClient.Do(req)` の呼び出し元に timeout があるか
- production の生の `go func(` 9 か所に recover が無い (done に再発事例が無いので候補にしていない)

## 低い確度で残すもの (issue にしていない)

- push の境界の `origin` の表示が固定 (別の remote 名の repo)
- `UnpushedSHAs` が失敗すると nil を返し、一覧が緑の「すべて push 済み ✓」を出す。失敗させる経路が見つからず**未確認リスク**

## 進捗

- [x] 6 波の監査と照合
- [ ] 834〜843 の反証レビューと訂正
