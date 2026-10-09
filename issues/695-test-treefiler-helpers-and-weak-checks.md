# 695 (test): treefiler のテストの補助の共通化と、検知力の弱い検査

起票日: 2026-10-09

## 概要

697 の監査のうちテストの分。母集合は `src/treefiler/filer/*_test.go` の 8 ファイル (約 90 本) と `src/glogx/filer_view_test.go` (14 本)。
**消してよいと言い切れるテストは無かった** (変異 13 本で確かめた)。

## 詳細

検知力の弱いもの:

1. `TestHeatStops` (filer_test.go) — ember の表を自分自身と照合している。`palette.go` の `heat` の補間 (smoothstep) を線形にしても、ember[0] の色を
   変えても全テスト green (変異で確認)。補間の形を固定する assert に直す
2. `exec_boundary_test.go` (`TestNoOsExecImport`) の下限 `scanned < 12` が古い。非テストの `.go` は 22 件 (find で数えた)。走査の根が壊れて 10 件
   落ちても緑。下限を上げるか、depguard の deny に置き換える (696 の lint 案)
3. git を使うテストが `t.Skip("git が無い")` で緑になる (6 箇所)。手元と CI (macos-15) には git があるので今は実害なし。CI で SKIP が 0 件なことを見るか Fatal にする

補助の共通化 (実数は走査した 9 ファイルでの数):

4. 本物の git の repo を作る closure が 6 コピー (うち 5 は既存の `gitIn` で置き換えられる)、`commit -c user.name=t ...` の並びが 5 箇所
   (`commitAll` を使っていない所が 4)。`initRepo(t, dir, branch)` を足す
5. `exec.LookPath("git")` の Skip が 7 箇所 → `needGit(t)`
6. 10 秒の上限 + 5ms の刻みで裏の処理を待つループが 6 箇所 (`settleOpen`・`settleBackground`・Busy 待ち・`waitExplode`・`waitDiff`・search_test.go の
   手書きのコピー)。`waitFor(t, msg, cond)` に寄せる
7. `m.Advance(fixedNow)` + `m.snapAll()` が 7 箇所 → `settle(m)`
8. `m.set.Git = false` + `setGit(m, ...)` が 4 箇所 → `setGit` に Git off を含める
9. `New(dir, Options{Now: fixedNow})` + `Resize(120, 40)` が `newTest` の外で 5 箇所 → `newAt(t, dir)`
10. `os.WriteFile` + `t.Fatal` の定型が 21 箇所 → `mustWrite(t, path, body)`

## 関連

- 監査の記録: 697

## 進捗

- [ ] 未着手
- [x] 1 `TestHeatStops`: 各区間の 1/4 (対数の時間) の色を、spec の smoothstep の式 (0.15625) で固定した。線形・t*t・smootherstep の変異で赤
  (ember の色そのものは表の値なので固定しない)
- [x] 2 `TestNoOsExecImport`: 下限の定数をやめ、走査した件数を `go list` (GoFiles + CgoFiles + IgnoredGoFiles) と突き合わせる。走査の根から filer を外す変異で赤。
  depguard への置き換え (696 の 5) は採らない: 件数の問題はこれで消え、同じ判定を 2 実装にしない
- [x] 3・5 git の Skip 8 箇所を `needGit(t)` に。CI (`CI` が空でない) で git が無ければ Fatal、それ以外は Skip。PATH を空にした run で
  CI=1 なら FAIL・無しなら SKIP を確かめた (GitHub Actions が CI=true を付けることは実 run では未確認)
- [x] 4・6〜10 補助を `filer/helpers_test.go` に集めた: gitIn / commitAll / gitCommit / initRepo / needGit / waitFor (6 つの待ちのループ) /
  settle (10 箇所) / newAt (12 箇所) / mustWrite (33 箇所) と、setGit が Git を off にする形。テストの一覧は前後で同じ (117 件、`go test -list`)
- 敵対レビュー (sonnet、1 周): P1 / P2 なし。待ちのループの条件・Advance の順序・hang guard が旧と同じこと、置き換えで中身・権限・dir が
  変わっていないことを突き合わせてもらった。直した P3: setGit のコメントの重複。記録のみ: TestNoOsExecImport は WalkDir が拾って go list が
  拾わない所 (`_x/`・`.x/`・入れ子の module) に .go を置くと赤くなる (安全側)
- `make test` / `make lint` (src/treefiler) rc=0
