# 850 (bug): treefiler のテストが途中で死ぬと、$TMPDIR に空の treefiler-config-* が溜まる

起票日: 2026-10-21

## 概要

`src/treefiler/filer` のテストを 1 回回すたびに、`$TMPDIR` に空の `treefiler-config-*` が 51 個残る。2026-10-21 に
このマシンの `$TMPDIR` で 12,104 個 (どれも空・10/12 の 16〜17 時台に作られたもの) を見つけ、手で消した。

起票時のタイトルは「途中で死ぬと」だが、調べた結果、中断が無くても残る (下の「原因」)。

## 原因

- `filer/main_test.go` の `TestMain` は `os.MkdirTemp("", "treefiler-config-")` で設定の置き場を作り、`m.Run()` の後に `os.RemoveAll(dir)` で消す
- `filer/shell_pty_test.go` の `TestBangGivesTerminalBackOnPTY` は、テストのバイナリ自身を helper の子として起動する
  (`exec.Command(os.Args[0], "-test.run=^TestBangHelperOnPTY$")`。`job` の形では helper がさらに自分を 1 つ起こす)
- 子でも `TestMain` が走って dir を作るが、helper の `TestBangHelperOnPTY` は最後に `os.Exit(0)` で終わる (4 か所)。
  `m.Run()` から戻らないので、子の `TestMain` の `RemoveAll` に一度も届かない
- 中断 (SIGKILL・`-timeout`・panic) でも同じく残る (`m.Run()` から戻らない終わり方は全部同じ)

実測 (2026-10-21、使い捨ての `TMPDIR` で回した):

| 回したもの | 残った空の `treefiler-config-*` |
|---|---|
| `go test -count=1 ./filer/` を 1 回 | 51 |
| 続けて `-run TestBangGivesTerminalBackOnPTY` だけをもう 1 回 | 102 (+51) |

51 は helper を起こす回数と一致する: ケースは 33 (leader の形 15・job の形 18) で、job の形は helper が自分をもう 1 つ起こすので
15 + 18 × 2 = 51 (zsh があるマシンの値。zsh が無いとシェルの種類が 1 つになり、数が変わる)。

10/12 の 16〜17 時台は treefiler を触る commit (`68a9c892` 15:59・`87eeae19` 16:42) の時間帯で、テストや変異検証を
何度も回していた。12,104 / 51 ≒ 237 回ぶん (概算。割り切れないので、`-run` で絞った回や中断が混ざっている)。
置き場を作る TestMain は `53c58179` (10/09)、helper は `5a9c0fa6` (10/11) から入ったので、10/11 以降のどの run も残す。

## 影響

- `$TMPDIR` の件数が増える。`dirhelper` の毎日の掃除は、空の dir を 9 日間消していなかった (10/12 のものが 10/21 まで残った。理由は未確認)
- 起動時の削除の件数に効く (`src/tmpclean/README.md`・glogx の doctor の「一時領域」タブ)。1 件ずつは空で小さい

## 対応方針 (案)

1. **helper の子では設定の置き場を作らない**: helper は `cmd.Env = append(os.Environ(), …)` で起動されるので、親の `TestMain` が
   設定した `TREEFILER_CONFIG_DIR` は既に子へ渡っている (新しい受け渡しは要らない)。`TestMain` の冒頭で `TREEFILER_BANG_PTY_HELPER`
   (`bangHelperEnv`) が非空なら `MkdirTemp` を飛ばし、継承した `TREEFILER_CONFIG_DIR` をそのまま使う。原因そのものを消す形。
   🚨 判定を「`TREEFILER_CONFIG_DIR` が設定済みか」にしない (利用者が外から設定していたときに置き場を作らなくなる)。
   helper の子が空の `TREEFILER_CONFIG_DIR` を受けたときは、本物を触らないよう失敗にする (`MkdirTemp` に戻さない)。
   sandbox (`sb.Allow`) は子のプロセスの中だけで、子は fileops を呼ばないので影響しない
2. **中断でも残さない**: doctor の module にある `testtmp` (`src/doctor/testtmp`。`$TMPDIR/dotfiles-go-test/<prefix>.<pid>.<連番>` に作り、
   次の起動で死んだ pid の dir だけを回収する) と同じ形にする。treefiler は doctor に依存していないので、`testtmp` を共有の module へ
   出すか、treefiler の中に同じ形を持つかを決める (glogx は `internal/testenv` 経由で `testtmp` を使っている)

1 は今回の 51 個の主因に効き、2 は中断の残りにも効く。1 だけでも件数はほぼ止まる。

## 確かめ方

- 使い捨ての `TMPDIR` で `go test -count=1 ./filer/` を回し、`find "$TMPDIR" -maxdepth 1 -name 'treefiler-config-*' | wc -l` が 0 であること
- 回帰テストとして、helper を起こすテストの後に置き場が残らないことを検査する (残す変異で red になること)

## 未確認の観測

- 使い捨ての `TMPDIR` (このマシンの `dotfiles/tmp/…` の下) で回すと `TestFrameAllocBudget` が「1 フレームの確保 262 回 (上限 258)」で落ちた。
  `TMPDIR` を変えたことが確保の回数に効くのかは調べていない (この issue とは別件の可能性が高い)

## 関連ファイル

- `src/treefiler/filer/main_test.go` (`TestMain`)
- `src/treefiler/filer/shell_pty_test.go` (`TestBangGivesTerminalBackOnPTY` / `TestBangHelperOnPTY`)
- `src/doctor/testtmp/testtmp.go` (中断でも回収する共通の部品)
- `src/glogx/internal/testenv/testenv.go` (`testtmp` を使う例)

## 進捗

- [x] 原因の特定と件数の実測 (2026-10-21)
- [x] 直す (方針 1) — commit「treefiler: PTY テストの helper の子が $TMPDIR に設定の置き場を残さない」
  - `TestMain` は `TREEFILER_BANG_PTY_HELPER` が非空で、かつ起動の引数に `-test.run=^TestBangHelperOnPTY$` (`bangHelperRunArg`) があるときだけ
    helper の子として扱う。そのときは置き場を作らず、継いだ `TREEFILER_CONFIG_DIR` を使う (空なら exit 1)
  - 引数も見るのは敵対レビューの P2 への対応: env だけを外から設定して `go test ./filer/` を打つと、親が helper の分岐に入り、全テストが継いだ置き場
    (本物でありうる) で走った。今はその打ち方でも通常の経路 (一時 dir を作る) に入ることを実測した
  - 置き場の名前 (`treefiler-config-*`) で判定する案は採らない: `newTest` が `t.Setenv` で置き場を `t.TempDir()` に差し替えるので、helper の子が継ぐ名前はその形にならない (試して 33 ケースが落ちた)
- [x] 回帰テスト — `runBangOnPTY` は helper の子にだけ別の `TMPDIR` を渡し、終わった後にそこが空であることを assert する
  - 変異: helper の分岐を `if false && …` にすると、33 ケースすべてがこの assert だけで red (leader の形は 1 個・job の形は 2 個残る。他の失敗は 0)
- 実測 (使い捨ての `TMPDIR`、`go test -count=1 ./filer/`): 残った `treefiler-config-*` は 51 → 0
- 方針 2 (中断でも残さない) は**採らない**: 修正後に残りうるのは中断 (SIGKILL・`-timeout`・panic) の run だけで、1 run に 1 個の空の dir。
  `testtmp` を共有 module へ出すと doctor・glogx・treefiler (と replace を継ぐ module) の go.mod と workflow の paths に波及し、得るものに見合わない。
  溜まったら `bin/tmpclean` (cleanup-tmp-dir skill) で片付く。**再評価の trigger**: `$TMPDIR` の `treefiler-config-*` が数十個を超えて見つかったとき
- 未確認の観測 (`TestFrameAllocBudget` が使い捨ての `TMPDIR` で 262 回 / 上限 258) は修正後も同じく出た。既定の `TMPDIR` では通る。この issue とは別件として調べていない
