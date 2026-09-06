# bug: zsh は SIGTERM で EXIT trap を走らせない（`check_syntax.zsh` の一時領域が残る）

起票日: 2026-09-06
カテゴリ: bug
優先度: 低（実害は残骸のみ。ただし **repo 全体の zsh スクリプトに効く前提の誤り**なので記録価値が高い）
出典: /audit resource-leaks 2026-09-06（forge Minimum+）。私が独立に実測

## 実測（2026-09-06、zsh 5.9 / macOS）

`mktemp` して `trap cleanup EXIT` だけを張ったスクリプトへシグナルを送った結果:

| shell | TERM | INT | HUP |
|---|---|---|---|
| **zsh 5.9** | **残る** | 消える | 消える |
| bash | 消える | 消える | 消える |

**`trap ... EXIT` は「シェルによらず後始末を保証する」ものではない。**
bash の感覚で書いた EXIT trap は、zsh では TERM の経路だけ穴が開く。

## 該当

`scripts/check_syntax.zsh`（`#!/usr/bin/env zsh` + `trap cleanup EXIT` のみ）。
`cleanup` は `tmp_log`（`mktemp`）と `tmux_tmpdir`（`mktemp -d`）を消すが、TERM では走らない。

同じ穴は **repo の zsh スクリプト全体**に潜む。

## 却下した懸念（再生成を止めるための記録）

「孤児 tmux サーバへ発展するのでは」→ **しない**。`_tmux.conf` は `exit-empty` が既定 on なので、
セッション終了でサーバも落ちる。残るのは一時ディレクトリだけ。

## 横断調査（2026-09-07。着手時に機械で数え直した）

「zsh の shebang かつ EXIT trap だけを張る」ファイルは **17 本**:

| 内訳 | 本数 |
|---|---|
| `scripts/` の production スクリプト | **1**（`check_syntax.zsh`） |
| `tests/` 配下 | 16（nvim 4 / zshrc 8 / setup 1 / tmux 1 / helper 2） |

**本 issue の commit で直したのは `scripts/check_syntax.zsh` の 1 本だけ**。
残る 16 本はテストで、**常用の中断は Ctrl-C = INT（zsh でも EXIT trap が走る）**なので
優先度が下がる。TERM を受けるのは timeout / 外部 kill の経路に限られる。

→ 16 本は下記の gate（`mktemp` と trap の対応を機械で見る）で一括して扱う方が安全。
個別に 16 本へ trap を書き足すと、`trap '' TERM` が正当なケース
（`tmux_server_watchdog.sh` / `tmux_schedule_keys.sh:cmd_fire`）との区別を人が覚えることになる。

## 対応済み（2026-09-07）

`scripts/check_syntax.zsh` に `TERM` / `INT` / `HUP` の trap を足した。

**変異検証**（同条件で比較。kill は一時領域が実際に作られたのを**上限つきポーリングで待ってから**送った
— 待たずに送ると mktemp 到達前に殺してしまい、修正の有無で結果が変わらない観測になる）:

| | kill 時点の一時領域 | TERM 後の残骸 |
|---|---|---|
| 修正後 | 2 個 | **0 個** |
| 変異（足した trap を消す） | 2 個 | **2 個** |

## 推奨対応

1. `check_syntax.zsh` の `trap cleanup EXIT` に加えて
   **`trap 'cleanup; exit 143' TERM` / `130 INT` / `129 HUP`** を並べる
   （同 repo の `scripts/run_make_targets_parallel.sh:30-32` が先例）
2. 横断検査を `tests/` に足す: **`#!/usr/bin/env zsh` かつ EXIT trap だけを張るファイル**を落とす
   - 🚨 `bin/lib/go_autobuild.zsh:492` のように**先に `trap '' HUP TERM INT` を張る形は正当**なので、
     allowlist ではなく「**TERM を無視しているか、TERM を trap しているか**」の二択で判定する
3. この事実を `_claude/rules/` に 1 行残す。**新規ルールは立てない** —
   [`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) の
   「異常系を実験で作る」表に「shell によって EXIT trap が走るシグナルが違う」を足すのが切り出し先
   （発動点が既存ルールと同じなので、CLAUDE.md の「既存ルールへの追記を既定にする」に従う）

## あわせて検討（同ファミリーの提案）

`scripts/` には shell の footgun を機械で止める gate の系列がある
（`check_pipefail_grep_q.sh` / `check_cd_rc_in_tests.sh` / `check_skip_exit_code.sh` /
`check_trigger_log_writers.sh`）。ここへ **「`mktemp` した変数が EXIT trap の掃除対象に入っているか」**
を見る gate を足すと、issue 298 / 299 / 305 の同型が再生産されなくなる
（今回の監査だけで `ui_run` と `check_go_project_lanes.sh` の 2 件が同型だった）。

🚨 **ただし字句 gate なので**
[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §8 に従い、
**脅威モデル（うっかり書く典型形を止める / 意図的迂回は review の責務）と「検出しない形」を
ヘッダに先に書く**こと。canary は本走査と同じ関数を通すこと。gate 自体が自作の安全機構なので、
新設するなら異常系の実験と敵対レビューを最終ゲートに通す。

## 関連

- issue 299（`ui_run` の同型）/ issue 305（Go テスト側の同型）
