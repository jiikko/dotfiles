# 945 (test): treefiler の列の検査が fixture の外へ広がり、$TMPDIR が大きいと終わらない

起票日: 2026-10-28

## 概要

`make test` の `src/treefiler/filer` で `TestColumnMatchesLayout/seed=9` が 10 分の timeout まで CPU 100% で動き続けた (pid の RSS は 322096 KB のまま不変)。
この機の `$TMPDIR` (`/var/folders/…/T`) の直下が **51,238 件**にたまっていたのが引き金で、テストが fixture の外の実在の dir を読む非 hermetic な作りだった。直した。

## 原因 (観測で確認)

- timeout のダンプ: `columnByLayout` の `sort.Slice` の中で、並べる列が約 5 万件 (`partition` の添字 0xc846 = 51,270)。
- 機構: カーソルが root のときの `h` は `rerootUp` で root を 1 段上げる。乱数の歩行が 2 回上げると root が実在の `$TMPDIR` になり、配置が **51,253 節**になる (直接測定)。
  検査の正解役 `columnByLayout` は節ごとに配置を作り直す O(n²) で、1 回 59 ms (-race)。節の数だけ呼ぶので約 50 分かかる計算。
- 再現 (決定的): `$TMPDIR` が 51k 件の状態で `go test -race -run 'TestColumnMatchesLayout$'` は 2 分でも終わらない (seed=9)。小さい `$TMPDIR` では seed=9 が 1 秒。
- 🚨 `go test` は `GOTMPDIR` をテストの `t.TempDir()` にも使う。`GOTMPDIR` を小さい dir にして「実 $TMPDIR で再現しない」と読みかけた (最初の調査の誤り)。実 `$TMPDIR` を使うには `GOTMPDIR` を外す。
- 負荷や乱数の揺れではない。`$TMPDIR` の件数だけで決まる。`$TMPDIR` の直下の内訳は `obaket-copy-tests-*` 1,180・`obaket-file-bodies-*` 774・`ibtoold-*` 441・`treefiler-config-*` 310・`obaket-thumbnails-*` 277 など、他の作業の残骸。

## 対応

- `column_test.go`: root を上げる `h` は、root が fixture の親 (`t.TempDir` が作る fixture だけを入れた dir) になったら `j` に替える (`capAscent`)。1 段目は通すので、root を上げる動きは検査に残る。
- 木の節の数が 300 を超えたら、ハングせずその場で赤くする (`escapedFixture`)。メッセージにキー列・節の数・root を出すので、次に逸脱したときの原因が読める。
- 検証: 同じ 51k 件の `$TMPDIR` で、2 分以上のハングが 3 秒で通る。変異 2 本 (`capAscent` が何もしない / 検出が常に false) で、前者は 6 秒でメッセージ付きの赤 (ハングしない)、後者は専用のテストが赤。`make -C src/treefiler lint` / `test` 成功。

## 残るリスク (未確認。観測点)

- `virt_test.go` の `TestVirtualizedRandomOpsMatchFull` (`-race` では skip)・`render_ref_test.go` の `h` を含む歩行も、root を上げれば実在の dir を読む。正解役は線形なので終わらなくはならないが、`$TMPDIR` が大きいと遅くなりうる。現状の `$TMPDIR` では通っており、今回は触っていない。
- 他の Go テストが `t.TempDir()` の外 (親の dir) を読んでいないかは調べていない。`make test` の Go の run だけを専用の空の `TMPDIR` で回す案は、他のテストへの影響が読めないので採っていない。
- **この機の `$TMPDIR` の 5 万件自体は、他の作業の残骸で、ここでは消していない**。片付けるなら skill cleanup-tmp-dir (`bin/tmpclean`) で数えて承認を取る。
