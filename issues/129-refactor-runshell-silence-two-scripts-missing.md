# 129 refactor: 長時間 detach する残り 2 本が無音契約を持たない

起票日: 2026-08-28 / 出典: issue 111 の敵対的レビュー / 優先度: P3

## 事実

`tests/tmux/test_runshell_silence.sh` が固定している基準
「**run-shell -b で detach され、長く生きるスクリプトは `exec </dev/null >/dev/null 2>&1` を持つ**」
に当てはまるのに、リダイレクトを持たない 2 本がある:

| スクリプト | 起動 | 生存時間 |
|---|---|---|
| `scripts/tmux_schedule_keys.sh` (`cmd_fire`) | `tmux run-shell -b` | `sleep "$wait_s"` — **上限 30 日** (`src/schedkeys/timespec.go` の `maxDuration`) |
| `scripts/tmux_resurrect_debounced_save.sh` | `_tmux.conf:507` の `command-alias[100] debounce-save` | `sleep "$(tt_debounce_seconds)"` — 既定 10 秒 |

`grep -rn 'exec </dev/null' scripts/ bin/` のヒットは 3 本のみ (111 で揃えた分を含む)。
`schedule_keys` の fire は restore の **5 桁長く**生きる。

## なぜ 111 で直さなかったか

- `scripts/tmux_schedule_keys.sh` は**起票時点で並行セッションが編集中**だった
  (`git status` で ` M`)。同一 working tree で書き込みを重ねない規律のため触らなかった
- `tmux_resurrect_debounced_save.sh` は `tt_on_default_server` を**関数の中**で呼ぶ構造で、
  他 3 本のような「gate の直後に置く」形が機械的に決まらない。既存テスト
  (`tests/zshrc/tmux-session/test_debounced_save.sh`) との突き合わせが要る

## 着手するときに知っておくこと (111 の実測。再測定しなくてよい)

隔離ソケットで測った結果 (tmux 3.7b / macOS):

| 子プロセスの振る舞い | pane が view-mode になるか |
|---|---|
| 無音 + rc=0 | ならない |
| **stdout 1 行** | **なる** |
| stderr (1 行 / 50 行 / 遅延) | **ならない** — tmux サーバの fd2 は `/dev/null` |
| **出力なし + rc≠0** | **なる** |
| `exec` あり + rc≠0 | **なる** |

つまり:

- `2>&1` は view-mode 対策としては**空振り** (揃えるためだけに付いている)
- **支配的な要因は rc≠0 の方**で、`exec` では塞げない。この 2 本を触るときは
  `set -e` 相当で落ちる経路が無いか (unbound variable / 関数リネーム) を先に見ること
- 「SIGPIPE を避ける」「stdin ブロックを避ける」は**どちらも実測で崩れた**理由。
  run-shell の子の stdin は即 EOF (`read -t 2` が rc=1)。書かなければ SIGPIPE も来ない

## 対応

2 本に `exec </dev/null >/dev/null 2>&1` を入れ、`tests/tmux/test_runshell_silence.sh` の
`targets` に足す。入れる位置は「実処理 (`tt_trigger_log` 等) より前」— テストがそこを見ている。
