# 079 refactor: 共有観測ログ (tt-restore-trigger.log) の書き手が 10 箇所に分裂 / rotation が periodic_save 依存

起票日: 2026-08-21
種別: refactor
優先度: **P2** (行書式は読み手向けの契約。書き手が散っていると片方だけ直る)

出典: 監査 [071](done/071-research-design-audit-2026-08-20.md) の `071-triglog-writer-count` /
`071-triglog-rotation`、[070](done/070-research-quality-audit-2026-08-20.md) の
`070-resurrect-inline-log` / `070-extra-shared-log-gate-failopen`。071 では **5 エージェントが
独立に指摘**した最も強いシグナル。
**出典 issue には「反証で崩れた (却下)」の一覧がある** — この項目のうち
`071-triglog-seam-bypass` (seam 迂回が false green の温床) は**反証で崩れている**。集約の価値は
残るが「テストが誤緑になる」を根拠に使わないこと。

## 確認できた事実 (2026-08-21 の grep)

`~/.cache/tt-restore-trigger.log` への 1 行追記が 10 箇所に分かれている。

| 形 | 箇所 |
|---|---|
| `TT_TRIGGER_LOG` seam + 同一実装の `log_line()` | `tmux_log_kill_command.sh:47` / `tmux_restore_runner.sh:34` / `tmux_verify_restore.sh:34` / `tmux_periodic_save.sh:54` |
| seam + printf インライン | `tmux_log_conf_source.sh:36` / `tmux_log_session_closed.sh` |
| **seam を無視して `$HOME/.cache/...` を直書き** | `tmux_resurrect_save.sh:210,216` / `tmux_reap_orphan_servers.sh:173` |
| `_tmux.conf` のインライン hook | `_tmux.conf:485,488` |

rotation (`TT_TRIGGER_LOG_MAX_LINES`) は `tmux_periodic_save.sh:43,86-90` にしかない。
= 共有ログの上限が「periodic_save が回っているか」に暗黙依存している。

## 下がる複雑性

- 書式変更・rotation 追加の touch 箇所が 10→1
- seam を尊重する版 / 迂回する版の 2 系統が 1 系統になり、テストから観測できない書き込み経路が消える
- 集約先の `scripts/lib/tmux_resurrect_guards.sh` は上記対象がすべて source 済み
  (自身のヘッダで「二重定義を避ける集約点」と役割宣言済み) なので**新しい依存辺は増えない**

## 対応方針 (着手時に再確認)

1. guards.sh に `tt_trigger_log <line>` を置き、seam (`TT_TRIGGER_LOG`) と rotation を
   そこへ集約する。rotation を全書き手が通る場所へ動かすと**書き込み毎に `wc -l` が走る**ので、
   確率的間引きか periodic_save 側に残すかを決めてから入れる (性能の判断が要る)
2. `_tmux.conf:485,488` のインライン hook は、同ファイル 428-431 のコメントが自ら
   「インラインで書かないこと (default socket ゲートを通す)」と禁じている形。スクリプトへ寄せる
3. `070-extra-shared-log-gate-failopen` (ゲートの fail-open) は 2 と同時に見る

## 変異検証

`tt_trigger_log` を no-op にすると、既存の観測ログ assert (6 本以上のテストが
`TT_TRIGGER_LOG="$LOG"` の慣行で書いている) が red になることを確認する。
`$HOME` 直書きだった 3 箇所については、seam へ寄せた後に**その経路専用の assert** を足す
(寄せただけでは既存テストが触っていないので緑のまま)。

## trigger

観測ログの書式を変えるとき、rotation を触るとき、または `_tmux.conf` の resurrect hook を
次に触るとき。単独でも着手できる (依存が増えないので risk は小さい)。

## 対応 (2026-08-25)

スクリプト側の書き手を `tt_trigger_log` (`scripts/lib/tmux_resurrect_guards.sh`) に集約した。
**`_tmux.conf` のインライン hook (項目 2) は見送り**。理由は下記。

### 実施

- guards.sh に `tt_trigger_log <本文>` を新設。seam (`TT_TRIGGER_LOG`) と行書式
  (`ISO8601 <TAB> 本文`) の正本をここ 1 箇所にした
- 逐語同一だった `log_line()` 4 本 (log_kill_command / restore_runner / verify_restore /
  periodic_save) を削除して寄せた
- printf インライン 2 本 (log_conf_source / log_session_closed) を寄せた
- **seam を迂回していた 3 箇所を潰した**: `tmux_resurrect_save.sh` の `tt_save_log` /
  `tt_save_log_guard` (`$HOME/.cache/...` 直書き) と `tmux_reap_orphan_servers.sh`
- guards.sh のヘッダの「利用者」記述が古かった (2 スクリプトと書いてあるが実際は 11) ので直した

### rotation の判断: **periodic_save に残す**

書き込みのたびに刈ると 1 行ごとに `wc -l` の fork が乗る。periodic_save は元々周期実行されて
いて追加 fork ゼロで刈れる。増加は実測 96 行/日 ≒ 8KB/日で、上限は forensics の保持期間を
決めるものであって安全機構ではないため、periodic_save が止まっている間に超過しても実害は無い。
**この「暗黙の依存」を `tt_trigger_log` のコメントに明示**して、暗黙→明示に変えた。

### ⚠️ issue の前提が 1 つ誤っていた

「集約先の guards.sh は上記対象がすべて source 済みなので**新しい依存辺は増えない**」は
**9 本中 8 本でしか成立しない**。`tmux_reap_orphan_servers.sh` は `#!/bin/sh` で guards.sh を
source しておらず、しかも **guards.sh は herestring (`<<<`) を使う bash 専用**
(`dash -n` が構文エラー)。source すると `/bin/sh` が dash の環境 (Linux CI) で壊れる。

→ このファイルだけは共有関数を使わず **seam (`TT_TRIGGER_LOG`) を尊重する形**に留めた。
issue が問題にしていたのは「seam を迂回してテストから観測できない」ことで、共有関数を通ること
自体ではないため、目的は達している。理由はコード直近のコメントに残した
(guards.sh が POSIX 化されたら寄せる)。

### 見送り: `_tmux.conf` のインライン hook (項目 2)

`@resurrect-hook-pre-restore-all` / `post-restore-all` は **復元の hot path** で、tmux の
run-shell 文脈で評価される。スクリプトへ寄せるには実際の復元を 1 周させて確認する必要があり、
本番の tmux サーバを使わずに安全に検証する手立てが今は無い。**復元経路を触るときの trigger 待ち**
として残す (issue 本文の trigger と同じ)。

### 変異検証

`tt_trigger_log` を no-op (`{ :; }`) にすると、観測ログを assert する 4 本
(periodic_save / log_session_closed / log_kill_command / restore_runner) が **4/4 red**。

⚠️ 迂回していた 3 箇所 (resurrect_save × 2 / reap_orphan) は、寄せただけでは既存テストが
触っていないので緑のまま。**その経路専用の assert は未追加**で、issue の「変異検証」節が
要求していた分が残っている → 次に resurrect_save の観測ログを触るときに足す。

## 残課題

- [x] `_tmux.conf` のインライン hook をスクリプトへ寄せる (2026-08-25 完了)
- [x] seam へ寄せた 3 経路 (resurrect_save × 2 / reap_orphan) 専用の assert (`292f5e8`、並行セッション)
- [x] `tmux_server_watchdog.sh` の 3 箇所のインライン書き手 (2026-08-25 完了)

## 完了の内容 (2026-08-25)

### これは refactor ではなく不変条件違反だった

`scripts/CLAUDE.md`「共有の観測ログに書くスクリプトは default socket ゲート
(`tt_on_default_server`) を通す」に対し、**`_tmux.conf` の 2 つの復元 hook だけが唯一の例外**で、
`restore-start` / `restore-end` を無ゲートで本番ログへ書いていた。この 2 行はまさに watchdog の
verdict が相関を取る行なので、`-L` の隔離テストサーバが復元すると**本番の死因分類が汚れる**。
前例として `scripts/tmux_log_conf_source.sh` が同じ理由で先に抽出されている。

### やったこと

- `scripts/tmux_log_restore_hook.sh` を新設 (`pre` / `post`)。ゲートを通し `tt_trigger_log` で書く
- **tmux オプションの設定は conf のインラインに残した**。`@tt-restore-complete` は
  「復元が最後まで到達した」の唯一の証拠で `tmux_restore_runner.sh` の途中死判定の入力。
  スクリプトへ寄せるとパス解決の失敗が「復元は成功したのに途中死と記録される」に化ける。
  ログはゲートしたい / オプションは絶対に落としたくない、で要求が逆なので分けてある
- `tmux_server_watchdog.sh` の 3 箇所 (`snapshot-health ng` / `ok` / `server-death`) を
  `tt_trigger_log` へ。`server-death` は**死亡検知時の時刻**で打刻する必要があるため
  (verdict の計算はその後)、`tt_trigger_log` に打刻の任意引数を足した
- **再発を静的検査で止める**: `scripts/check_trigger_log_writers.sh` (`make test-trigger-log-writers`、
  `test-lint` に配線)。書き手は guards.sh のみ。`#!/bin/sh` で guards.sh を source できない
  `tmux_reap_orphan_servers.sh` は行内 `trigger-log-writer: allow` で明示的な例外

### 検証

- `snapshot-health ng` / `ok` は**どのテストも見ていなかった**ので `tests/tmux/test_server_watchdog.sh`
  に状態遷移 (異常→回復) の assert を追加。新設スクリプトは `tests/tmux/test_log_restore_hook.sh` (5 観点)
- 変異 6/7 red: ゲートを外す / restore-start を書かない / restore-end を書かない /
  duration の数値検証を外す / 異常を記録しない / 回復を記録しない
- 静的検査は**守っている修正を revert して赤くなることを集約経路 (`make test-lint`) で確認**した
  (conf に生の書き手を戻すと `test-trigger-log-writers` が該当行を指して落ちる)

### 固定できなかったもの (記録)

- `server-death` の打刻が「死亡検知時」であること。`tt_trigger_log` の打刻引数を無視する変異は
  **green のまま通る**。`%FT%T` は 1 秒解像度で、検知から書き込みまでの verdict 計算は
  サブ秒なので実測上ほぼ同値になるため。観測可能にするには production 側に遅延の seam を
  入れる必要があり、テストの都合で本番を歪めるので採らなかった
