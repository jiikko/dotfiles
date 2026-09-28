# 574 (ux): bin/mutate-verify が baseline と変異の出力の差を黙って 40 行で切り、全体を後から読めない

起票日: 2026-09-28

## 概要

`bin/mutate-verify` は rc=0 (想定の検査が red) のとき、巻き添えで落ちた検査が無いかを人が見るために
「baseline と変異の run の出力の差」を表示する (冒頭の射程の節「巻き添えで落ちた検査」)。
ところが表示は `bin/mutate-verify` の末尾の

```sh
{ diff -a "$base_out" "$mut_out" || true; } | grep -a '^[<>]' | head -40 >&2
```

で **先頭 40 行に切られ、切ったことも全体の行数も出ない**。しかも `$base_out` / `$mut_out` は使い捨て
worktree の gitdir (`mv-baseline.log` / `mv-mutant.log`) にあり、終了時に `scripts/lib/worktree_scratch.sh` が
worktree ごと消すので、**全体を後から読む手段が無い**。

切ること自体は冒頭に「先頭 40 行で切る」と書かれた意図的な設計 (無関係な揺れの行が混ざるため)。
問題は、切れた先に**見たい行 (巻き添えで落ちた・消えた検査) があっても、表示からは「無い」と区別できない**こと。
「必ず表示する」と言っている材料が、長い出力では黙って欠ける。

## 詳細 (踏んだ例)

issue 572 の変異検証 (`--verify 'bash tests/githooks/test_pre_push.sh'`、失敗時に git push の stderr を丸ごと出すテスト) で、
「落ちても exit 0」の変異 (m3) を当てたとき:

- 表示された差は 40 行で終わり、ケース 6 は「消えた行」(`< ✓ ...`) だけが出て、対になる `✗` 行とテスト末尾の
  `✗ pre-push: N 件失敗` が出なかった。テストが途中で止まった (巻き添え) ようにも読めた
- 使い捨て worktree を手で作って同じ変異を当て直し、全出力を見て、実際は 3 ケースとも `✗` を出して最後まで走っていたと分かった
  (往復 1 回分)

rc=6 (変異が緑) の `tail -20 "$mut_out"`、rc=7 の `grep ... | head -10` も同じく切るが、そちらは
「なぜその判定か」の手がかりであって、判定の根拠を人に委ねている rc=0 の差ほど重くない。

## 対応方針 (案)

1. 切ったときは `(… 残り N 行を省略。全体: <path>)` を必ず出す。全行数は `diff | grep -c` で取れる
2. 全体を後から読めるように、差 (または `$base_out` / `$mut_out`) を worktree の外 (呼び出し元の scratch や
   `--keep-logs <dir>` のような明示の出力先) へ写してから後片付けする。写し先を消す責任の所在も決める
   (repo の `./tmp` か OS の一時領域か。CLAUDE.md の「一時ファイルの配置」)
3. 40 行という上限自体は変えなくてよい (揺れの行で埋まるのは意図どおり)。欠けたことが見えれば足りる

## 受け入れ条件

- [x] 差が上限を超えたとき、省略した行数と全体の置き場が出力に出る
- [x] 終了後も全体の差を読める (置き場が消えていない)
- [x] `tests/bin/test_mutate_verify.sh` に、41 行以上の差が出る変異で省略の表示が出ることを見るケースを足す

## 関連ファイル

- `bin/mutate-verify` (末尾の差の表示と、冒頭の射程の節「巻き添えで落ちた検査」)
- `scripts/lib/worktree_scratch.sh` (worktree と gitdir の後片付け)
- `tests/bin/test_mutate_verify.sh`

## 進捗

- 2026-09-28: 起票。反証レビュー (sonnet 1 体、読み取り専用): 指摘なし。引用行 (末尾の差の表示・rc=6 の tail -20・rc=7 の head -10)、gitdir の後片付け (`wts_cleanup`)、同じ問題を扱う issue やログを残すオプションが無いこと、テストに 41 行超のケースが無いことは裏が取れた
- 2026-09-28: 実装 (commit「mutate-verify: 出力の差を切ったら残りの行数を出し、全体のログを worktree の外に残す (574)」)
  - `bin/mutate-verify`: `save_logs` が baseline / 変異 / 構文検査 (変異前・変異後) のログと差の全体 `diff.txt` を
    `MUTATE_VERIFY_LOG_ROOT` (既定 `$TMPDIR`) の `mktemp -d` へ写し、`finish` と EXIT trap (中断) から 1 回だけ置き場を出す。
    差の表示が 40 行を超えたら「残り N 行を省略」と出す。写せなくても rc は変えない。道具は置き場を消さない
  - 起動時の stderr を fd 3 に控え、`note`・rc=9 の警告・中断時の後片付けの出力をそこへ書く。bash は関数の実行中に届いた
    シグナルの trap を、その関数のリダイレクト (`run_in_wt ... > "$mut_out" 2>&1`) がかかったまま走らせるので、
    中断時の報告 (rc=9 の警告・「worktree を消せなかった」を含む) が mv-mutant.log へ入って worktree と一緒に消えていた。
    **この修正の前から在った穴** (中断時の rc=9 の警告も消えていた)
  - 置き場が元 repo の中なら `$TMPDIR` へ倒す (写した物が untracked に見えて rc=9 を作るため)。判定は親をたどって `-ef` で比べる
  - `_claude/rules/mutation-verify-new-tests.md`: 40 行を超えた分は「全体のログ」の `diff.txt` にある、を 1 行足した

## 結果

- `tests/bin/test_mutate_verify.sh` は 41 ケース (37〜42 と末尾の漏れの検査を足した)。テスト中の置き場は `$work/logs` に向け、
  各 run の置き場が `$work` の外にできたら NG (`$TMPDIR` 全体を数えないのは並行する別 session の run を拾わないため)
- 変異検証 (`bin/mutate-verify`。どれも狙った NG で red): 省略の表示を消す / finish から保存を外す / LOG_ROOT を無視する /
  写せないと rc=3 にする / 元 repo の中の拒否を外す / 中断の経路から保存を外す / note を stderr に戻す / `head -41` /
  `-lt 40` / `-ef` を文字列の比較に戻す
- 敵対的レビュー (opus、1 体ずつ 3 周):
  - 1 周目: P2 LOG_ROOT が元 repo の中だと rc=0 が rc=9 に (→ 検査の後に写す + 中なら倒す) / P2 中断でログが残らない
    (→ EXIT trap からも写す。調べる中で上の fd 3 の穴が見つかった) / P2 末尾の残骸検査が並行 session の run を拾う
    (→ run ごとに置き場が `$work` の下かを見る) / P3 40・41 行の境目が無検査 (→ ケース 40) / P3 変異後の構文検査の出力が
    残らない (→ mv-syntax-mutant.log) / P3 長い・改行を含む --name (→ 安全な文字と 100 文字に丸め、mktemp の失敗理由を出す)
  - 2 周目: P2 中断時の「worktree を消せなかった」警告が消える (→ on_cleanup の先頭で `exec 1>&3 2>&3`) / P3 定義前の中断で
    command not found (→ 定義を wts_init の前へ) / P3 相対の LOG_ROOT が経路でずれる (→ 起動時に絶対化) /
    P3 大文字小文字で判定を抜ける (→ 3 周目で置き換え)
  - 3 周目: P2 小文字化の比較が NFD・C locale で抜ける → 文字列の比較をやめ、親をたどって `-ef` で比べる (答えを FS に出させる) /
    P3 倒す先の $TMPDIR も相対なら経路でずれる (→ 起動時に絶対化) / P3 表記の検査が区別する volume で黙って skip (→ skip と出す、
    NFD と C locale の変種も足す)。
    **4 周目は回していない**: 3 周目の修正は環境依存の文字列の判定を FS の同一性へ置き換えたもので、3 周目が実測した
    書き方 (大文字・NFD・C locale) はすべてケース 41 で固定し、`-ef` を文字列の比較へ戻す変異で 3 つとも red を確かめた
- 採らなかった指摘 (記録): `$TMPDIR` 自体が元 repo の中なら倒した先も中 (wts_init も同じ TMPDIR に worktree を置く、元からの性質) /
  `run_in_wt` の `3>&-` を守るテストが無い (壊れても検証コマンドが道具の stderr に書けるだけ) /
  `$MV` を直接呼ぶ 5 ケースは置き場の漏れ検査の外 (LOG_ROOT を無視する変異は mv_run 経由の 42 件で検出される)

## 残タスク

- なし (受け入れ条件は全部満たした)
