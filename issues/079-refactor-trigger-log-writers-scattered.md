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
