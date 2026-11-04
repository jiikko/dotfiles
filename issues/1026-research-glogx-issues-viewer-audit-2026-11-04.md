# 1026 (research): glogx の issues viewer の全観点の監査 (2026-11-04。直接実行) — 全数勘定と、直さないと決めたもの

起票日: 2026-11-04

## 概要

`all-audit-on-diy` (audit skill の全タイプを直接実行し、その場で直す) を glogx の issues viewer に回す。
前回の [754](done/754-research-glogx-issues-viewer-audit-2026-10-13.md) (2026-10-13、起点 `f61d1e664`) 以降の差分を重点にする。

## 走査した母集合

- 範囲: `src/glogx/internal/issuesview` と `src/glogx/issues` (47 ファイル、うちテスト 28。非テストは 7,007 行) と `docs/issues-viewer-spec.md`
- 差分: `git diff -M f61d1e664..HEAD` で非テスト 17 ファイル +1256 / −765 行・38 commit。大半は 817 の package への移設と 754 の指摘 (748〜753) の修正。
  **監査を通っていない新しい挙動**は epic の親の判定 (c1dd755a3・6356e7d72・756b24a4e・5c2388f5e。`issues/parse.go` の
  `GroupParentRank`・`PreferGroupParent`・`groupOpenParents`・`closedGroupKeys`・`showsIssue`)、親行の案内 (c51ec537d)、一覧の `u` (5385ff22f) の 3 つで、ここを全部読んだ
- 全部を読んだ: `issues/parse.go` の差分全部・`issuesview/issues_view.go` の `rebuildDisplayRows`・`appendOpenHintItems`・`scanIssues`・`loadAllMeta`・`HandleKey` の一覧の分岐、
  `issues_watch.go` の差分 (移設で `StopWatch` が `fswatch.Chain.Advance` を通る形になった。`Advance` が `Close` を呼ぶのを確かめた)
- **読んでいない差分**: `issues_view.go` の移設の差分のうち上に挙げた関数の外・`issues_linkjump.go`・`url_picker.go`・`issues_number_filter.go`・`issues_state.go`
  (どれも名前の公開化と package の付け替えが主で、754 の後に挙動を足した commit が無い)。`move.go`・`banner.go` は 748・753 の修正で、その時の変異と敵対レビューを通っている
- 機械の検査 (`053297f02`): `go vet` rc=0 / `go test -race -count=1 ./internal/issuesview/... ./issues/...` ok / `make lint` 0 issues /
  `deadcode ./...` で 2 package の報告 0 件 / `dupscan sweep internal/issuesview issues` (単位 425・46 ファイル)

## 観点ごとの結果

| 観点 | 結果 | 攻めた所・報告しなかった理由 |
|---|---|---|
| error-handling | 1 (直した) | `loadAllMeta` が `LoadMeta` の失敗を全部捨てていた。見出しの形の epic の親 (`# NNN (epic):`) は見出しが読めないと親と判定されず、理由が画面に出ない → 走査の警告と同じ行に出す (ENOENT は除く) |
| duplication | 1 (直した) | `rebuildDisplayRows` の 1 つ目の case が `PreferGroupParent(iss, nil)` と同じ判定で、親の選び方が viewer にも書かれていた。sweep の節 1 `Status.String` ~ `Badge` は default 無しの switch 2 本 (exhaustive が新しい状態を強制する。表にすると強制が消える) で寄せない。節 2 `TakeWantQuit` ~ `TakeWantPull` は `TakeWantCross` と同じ「一度きりの合図」の 3 行の形。節 3 `WriteRealIssue` ~ `filelink_test.go` の `writeFileContent` は、`issues` の package 内テストから `issuesviewtest` (issues を import する) を使えないので寄せられない |
| design / encapsulation | 0 | 親の判定の 3 つの集合 (open な親・終わった group・閉じた group) が同じ親を選ぶか: `groupOpenParents` は全件から `PreferGroupParent` で選び、viewer は見えている行から同じ関数で選ぶ。違う親になりうるのは「最強の親が伏せられ、弱い形の親が見えている」ときだけだが、弱い形の親が open なら `closedGroupKeys` がそれを子に数えて group は閉じない (伏せる条件が成り立たない) ので食い違わない。`retiredGroupKeys` が `IsGroupParent` (どの形でも) で親の done を見るのは、全員 done のときだけ効くので、選んだ親で見ても結果は同じ |
| concurrency / resource-leaks | 0 | 移設で `stopWatch` が `issuesWatch{Chain: v.watch.Advance()}` になった。`Advance` は `Close` してから世代を進める (`internal/fswatch`)。`-race` 緑 |
| security | 0 | `openIssueFile` (O_NOFOLLOW・O_NONBLOCK・通常のファイルの確認) は親ディレクトリの差し替えを見ないと doc に理由つきで書いてある。`ReadBody` が大きさの上限なしに全部読むのは手元の repo の issue だけが相手 |
| performance | 0 (見積もり) | `refresh` はタブごとに `issues.Filter(v.all, …)` を呼び、毎回 group の 3 集合を全件から作り直す (タブ数 × 4 × 全件。dotfiles の 1016 件・タブ 20 前後で 8 万回程度の map 操作)。キー・再走査のたびで、毎フレームではない。実測はしていない |
| false-green | 0 | 新しいテストは変異で red を確認 (5385ff22f・b3d70225e の commit message)。最初に書いた `loadAllMeta` のテストは ENOENT の変異で緑だった (警告が先頭の 1 件しか名指ししない) のを、この監査の中で直した |
| ci | 0 | `src_glogx.yml` の paths は go.mod の replace 12 本を全部覆う |
| ux | 0 (判断材料を報告) | 一覧の `u` (pull) は hint 行に出ない (`R`・`i` と同じく幅の予算の都合。案内は `--help` と README)。pull の確認は Enter が y で、viewer では Enter が最も押されるキーなので、誤って `u` を押した直後の Enter が pull を走らせる。git log 一覧と同じ確認の形にする依頼なので変えていない |
| dead-code | 0 | `deadcode` の報告 0 件 |
| broken-code | 0 | 移設の罠 (cwd 相対で読むテストの Skip / 0 件) は `structure_ratchet_test.go` と retro 815 の対象。今回の差分で `..` を読むテストは足されていない |
| responsibility / polymorphism / leaky-abstraction / ui-components / test-cleanup / test-helpers / dependency / lint-from-done / general | 0 | 754 から構造は変わっていない (817 で package を分けたが、責務の割り方は同じ)。test-helpers は sweep の節 3 の 1 組 (上の duplication の行) だけ |

全数: 直した 2 (error-handling 1・duplication 1)、起票 0。ユーザーの判断が要るもの 1 (ux の Enter と pull の確認)。

## 対応

- `loadAllMeta` が読めない issue を警告に返す / `rebuildDisplayRows` の親の選び方を `PreferGroupParent` だけにする (b3d70225e の 1 commit)

## 進捗

- 2026-11-04: 監査・対応
