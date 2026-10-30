# 997 (ux): Go の testdata の壊れた fixture で gate が毎回「解析が不完全」と出る

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-30〜）

起票日: 2026-10-30

## 概要

dotfiles の `src/subproc/boundarycheck/testdata/broken/broken.go` は、検査の道具のテストのためにわざと構文を壊したファイルである。
dupscan はこれも Go のファイルとして読むので、`dupscan gate src` (make test・CI・セルフレビュー) の 1 行目は毎回
`新しい重複 0 (読めたファイルの中で)・解析が不完全 (… 読めない構文のファイル base 1 / head 1 …)` になる (2026-10-30 の自分の gate でも出た)。

読めない構文のファイルを 1 行目に出すのは [929](done/929-bug-dupscan-gate-lost-file-parse-errors.md)・[959](done/959-research-dupscan-dogfood-feedback-obaket-1307-part3-2026-10-29.md) で決めた形で正しい。
困るのは、壊れていて当然のファイルが 1 つあるだけで、1 行目の状態の語が毎回「解析が不完全」になること。件数 (`base N / head N`) は出るので増えれば数字は変わるが、状態の語が常に同じなので読み流される。
[972](done/972-research-dupscan-dogfood-glogx-tui-split-2026-10-29.md) の F2 で `index -file` の側は「関係しうるファイルだけ」に直したが、gate と report は直していない。

## 発火条件

Go の `testdata` の下に読めない構文の `.go` があり、gate / report の起点に入る (dotfiles の `src` を起点にするとき毎回)。`go build` / `go vet` は `testdata` を読まないので、そこが読めないことは製品の写しの検査と関係しない。

## 対応方針

- 変えるのは **text の 1 行目の状態の語だけ**: 読めないファイルが全部 Go の `testdata` の下 (go のツールが読まない置き場) なら、状態を「新しい重複なし」にし、
  後ろに `・読めない testdata N (go が読まない置き場。-strict-analysis では数える)` と出す。testdata の外に 1 つでもあれば今までどおり「解析が不完全」
- **rc・json (`analysis_incomplete` / `*_files_with_errors`)・`-strict-analysis` は変えない**。[929](done/929-bug-dupscan-gate-lost-file-parse-errors.md) は
  この fixture で strict が rc=2 になることを検証済みの挙動として記録し、「既存の rc 契約は黙って変えず」とした (反証レビューの P2)。契約は保ったまま、人が読む語の雑音だけを減らす
- report の 1 行目 (`読めない構文があるファイル N`) も同じ語の扱いにする
- 採らない案: testdata を既定で読まない (972 の F2 で退けた形。読めないことを黙らせる) / gate の「確かめられなかった変わった単位」で状態を決める (929・959 の「読めないファイルがあるのに なし と言い切らない」を、testdata 以外にも緩めることになる)
- Swift には同じ慣習が無いので変えない

## 受け入れ条件

- [ ] testdata の下の壊れたファイルだけがある repo で、gate の 1 行目が「新しい重複なし」で、testdata の数が別に出ることをテストにする (直す前に赤)。json の `analysis_incomplete` と strict の rc=2 は変わらないことも見る
- [ ] testdata の外の壊れたファイルは今までどおり「解析が不完全」になる (対照)
