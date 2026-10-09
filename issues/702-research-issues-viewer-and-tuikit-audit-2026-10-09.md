# 702 (research): glogx の issues viewer と tuikit の監査 (2026-10-09。直接実行)

起票日: 2026-10-09

## 概要

`/audit` を 2 つの対象で回した。読み取り専用の調査体 (sonnet) 4 体を 1 体ずつ順に回し、main (Opus) が実コードで裏を取ってから issue にした。

| 調査体 | 対象 | 監査の種類 | 走査した母集合 | 起こした先 |
|---|---|---|---|---|
| 1 | issues viewer | security / resource-leaks / broken-code / dead-code / error-handling / false-green / performance / concurrency / ux / general | `src/glogx/issues_*.go` (非テスト 15) と `src/glogx/issues/`・tests/issues/*.sh | 698 |
| 2 | tuikit | design / responsibility / duplication / polymorphism / leaky-abstraction / encapsulation / ui-components | tuikit の非テスト 33 + 使う側 4 (glogx・pro-con・treefiler・schedkeys) | 700・699 の 3 |
| 3 | tuikit | security / broken-code / error-handling / dead-code / false-green / performance / concurrency / resource-leaks | 同上。termwidth を 30 万回・markdown を 4 万回・layout を 6 万回掃引 | 699・701 の 2 と 8〜12 |
| 4 | tuikit | test-cleanup / test-helpers / dependency / ci / lint-from-done / general | *_test.go 32 ファイル (194 本)・go.mod 7 つ・src_tuikit.yml の run・issues/done の tuikit への言及 26 件 | 701 |

issues viewer の設計 7 種とテスト 2 種は 10/07 に回した (668)。同じ結果になるので今回は回していない。

## 全数勘定

- 698 (bug 9 項。うち 2 項は記録)・699 (perf 3 項)・700 (refactor 6 項)・701 (chore 13 項)
- 0 件: data race (issues viewer は `go test -race` で、tuikit は -race と hlEscCache の sync.Map を確かめて 0)・fd の漏れ (issues viewer の監視は閉じると 730 → 10 に戻る)・
  panic (tuikit の掃引で 0)・表示幅の溢れ (issues viewer の幅 1..100 × page 5 通り × 7 モードで 0)・到達しないコード (`check_unused_excluding_tests.sh` rc=0)

## 攻めたが見つからなかった範囲 (次の監査の起点)

- issues viewer: ファイル名 / H1 / 宣言 / グループ名 / 警告 / リンクのラベルは termsafe を通る (通らないのは 698 の 3 だけ)。`../` を含むリンク・symlink 越し・
  `~/`・`%00 %1b` は、repo の外を指すなら拒否する。MoveToSubdir は上書きしない。dangling の目印はバナーの書き込みの失敗で巻き戻る。規模: 実 repo 696 件で
  走査 38ms、合成 5000 件で 154ms。fsnotify (kqueue) は issue 1 件に 1 fd で約 720 fd (上限 245760 で実害なし)
- tuikit: import は DAG で循環なし。部品は bubbletea を import しない (depguard / forbidigo が止める)。markdown の出力に ESC / C1 / `\r` は出ない (parseBlocks が
  全行を無害化)。toast は push の時に無害化。lineedit は 10 万字で 1 打鍵 4ms。chroma・x/ansi・bubbletea・ultraviolet・uniseg・displaywidth・colorprofile の版は
  使う側 6 module で一致。go mod tidy の差分 0
- 却下: 使う側 0 の export の多く (`anim.EaseOutBack`・`listnav.Fit` など。tuikit の内部の部品と定数で害が無い)・責務の集中 (toast.go 600 行・termwidth.go 592 行・
  markdown.go 591 行は大きいが、複数の観点の裏付けが取れなかった)・confirm に 3 値を持たせる案 (使う側が 1 つ)

## 進捗

- [x] 監査を実行し、発見を 698〜701 に起こした
