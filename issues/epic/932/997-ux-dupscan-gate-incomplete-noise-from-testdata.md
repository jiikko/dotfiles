# 997 (ux): Go の testdata の壊れた fixture で gate が毎回「解析が不完全」と出る

起票日: 2026-10-30

## 概要

dotfiles の `src/subproc/boundarycheck/testdata/broken/broken.go` は、検査の道具のテストのためにわざと構文を壊したファイルである。
dupscan はこれも Go のファイルとして読むので、`dupscan gate src` (make test・CI・セルフレビュー) の 1 行目は毎回
`新しい重複 0 (読めたファイルの中で)・解析が不完全 (… 読めない構文のファイル base 1 / head 1 …)` になる (2026-10-30 の自分の gate でも出た)。

読めない構文のファイルを 1 行目に出すのは [929](done/929-bug-dupscan-gate-lost-file-parse-errors.md)・[959](done/959-research-dupscan-dogfood-feedback-obaket-1307-part3-2026-10-29.md) で決めた形で正しい。
困るのは、壊れていて当然のファイルが 1 つあるだけで知らせが毎回出るので、**本当に読めないファイルが増えても 1 行目の見た目が変わらず気づけない**こと。
[972](done/972-research-dupscan-dogfood-glogx-tui-split-2026-10-29.md) の F2 で `index -file` の側は「関係しうるファイルだけ」に直したが、gate と report は直していない。

## 発火条件

Go の `testdata` の下に読めない構文の `.go` があり、gate / report の起点に入る (dotfiles の `src` を起点にするとき毎回)。`go build` / `go vet` は `testdata` を読まないので、そこが読めないことは製品の写しの検査と関係しない。

## 対応方針

- Go の adapter が「この言語のツールが読まない置き場」(Go は `testdata` の下) を持ち、そこの読めないファイルは `解析が不完全` に数えず、別に数える
  (1 行目には `・読めない testdata N (数えない)` のように短く出し、json にも別の欄で出す。黙って消さない)
- `-strict-analysis` も同じ数え方にする (testdata の壊れた fixture で rc=2 にしない)
- Swift には同じ慣習が無いので変えない

## 受け入れ条件

- [ ] testdata の下の壊れたファイルだけがある repo で、gate の 1 行目が「新しい重複なし」で、testdata の数が別に出ることをテストにする (直す前に赤)
- [ ] testdata の外の壊れたファイルは今までどおり「解析が不完全」になる (対照)
