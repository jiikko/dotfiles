# 790 (chore): tuikit の depguard の部品のリストが、実際の package を全部覆っているかを機械で見る

起票日: 2026-10-16

## 概要

`src/tuikit/.golangci.yml` の depguard には 2 つのリストがある。リストは手で並べていて、package を足すと漏れる。
issue 785 で足した `hintfit` も最初は漏れ、bubbletea を import しても lint が通った (opus の敵対レビューで見つかった)。

- `no-framework-in-parts`: bubbletea を禁じる
- `parts-pure`: os / os/exec / context を禁じる

## 詳細

2026-10-16 に `go list -f '{{.Imports}}'` と、2 つのリストを突き合わせた。母集合は `src/tuikit/` 直下の 21 ディレクトリ (cmd / examples は package ではない。`go list ./...` では `cmd/termwidth`・`examples/*`・`foreground/ptytest` を含む 23 package)。

| package | import | no-framework-in-parts | parts-pure | 判断 |
|---|---|---|---|---|
| gutter | termwidth, strconv | 無い | 無い | 両方に足す (純粋) |
| spinner | time | 無い | 無い | 両方に足す (純粋) |
| foreground | os, os/exec, os/signal, unix ほか | 無い | 無い (正しい) | no-framework にだけ足す (I/O が仕事。bubbletea は使わない約束 = README) |
| caret | bubbletea | 無い (正しい) | 無い | 意図して外す (bubbletea の View.Cursor を組むのが仕事) |
| framebench | ultraviolet, colorprofile | 無い | 無い | 意図して外す (部品ではなく計測の道具) |
| examples / cmd | — | 外す印あり | 外す印あり | 意図して外す |

残りの 14 package (anim confirm editor highlight hintfit layout lineedit listnav markdown sgr termcolor termwidth toast widthenv) は no-framework-in-parts に入っていた。parts-pure は、そのうち editor / termcolor / widthenv を意図して外している (I/O が仕事)。

## 対応方針

1. gutter / spinner を 2 つのリストに、foreground を no-framework-in-parts に足す (反証レビューが設定のコピーで lint を回し、足しても 0 issues を確かめた)。
   - `**/foreground/**` は `foreground/ptytest` (テスト用の pty の道具。context / os / os/exec を使い、bubbletea は使わない) にも当たる。no-framework に入っても通る
   - `spinner_test.go` は os / widthenv を使うが、parts-pure は `!**/*_test.go` で外しているので通る
   - `README.md` の「部品は bubbletea を import しない」の一覧にも同じ 4 つ (gutter / spinner / foreground / hintfit) が無い。同じ変更で直す
2. 漏れを機械で止める検査を足す: tuikit の全 package が、2 つのリストのどちらか、または「外す理由つきの表」に載っていることを見る
   (`tests/scripts/` か tuikit の Go のテスト。表は検査の中に持つ)。母集合は `go list ./...` で取り、`foreground/ptytest` などの入れ子も
   どちらかに載せる。今ある `test_tuikit_consumers_aligned.sh` は ansi の禁止の揃いだけを見ていて、depguard の網羅は見ていない
3. 一時ファイルで bubbletea / os を import して、足した package で depguard が出ることを確かめる (785 と同じ確かめ方)

## 関連ファイル

- `src/tuikit/.golangci.yml`
- `src/tuikit/CLAUDE.md` (地図)
- issue 785 (hintfit の漏れ)

## 進捗

- [ ] 反証レビュー
- [ ] 1. リストに足す
- [ ] 2. 漏れを止める検査
- [ ] 3. depguard が出ることの確かめ
