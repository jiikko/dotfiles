# 790 (chore): tuikit の depguard の部品のリストが、実際の package を全部覆っているかを機械で見る

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-16〜）

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

- [x] 反証レビュー (sonnet。母集合の数え方・ptytest・README の一覧を訂正した)
- [x] 1. リストに足す (commit「tuikit: depguard の部品のリストに gutter / spinner / foreground を足し…」)。README の「不変条件の守り」も実際に合わせた
  (parts-pure から外すのは editor / widthenv だけと書いていたが、termcolor も外れていた)
- [x] 2. 漏れを止める検査 `tests/scripts/test_tuikit_depguard_covers_parts.sh` (make test が自動で拾う)。
  - opus の敵対レビューを 3 周回した。1 周目は P1 1 / P2 2 / P3 1、2 周目は P2 1、3 周目は P2 1 / P3 1。直したのは次の 7 つ:
    - コメントアウトを「載っている」と読む
    - 知らない形の否定を読み飛ばす
    - root の package を落とす
    - 外す表を先頭の名前で引く
    - YAML の位置を見ない
    - 「-」の後の改行を読み飛ばす
    - 2 つ目の文書を足し合わせる
  - 字句の検査として、脅威モデルと「検出しない形」(どれも赤に倒れる側) をヘッダに書いて閉じた
  - 変異で赤を確かめたもの (11 本):
    - DROPGUTTER: リストから 1 行消す
    - COMMENT: リストの行をコメントアウトする
    - UNKNOWNNEG: 知らない形の否定を足す
    - XRULES: rule を rules の外へ出す
    - DASHNL: 「-」の後で改行する
    - MULTIDOC: YAML の文書を足す
    - STALE: 外す表に古い名前を足す
    - DROPCARET: 外す表から caret を消す
    - BOTH: リストと外す表の両方に載せる
    - NESTED: 入れ子の外す項目を消す
    - CANARY: 読み取りを壊す
  - tuikit 直下に .go を置く形は、一時ファイルで赤になることを確かめた
- [x] 3. 一時ファイルで gutter / spinner / foreground に bubbletea を、gutter に os を import し、depguard が 4 件とも出すことを確かめた (ファイルは消した)
