# 618 (design): Claude Code の mods (関数 hook の plugin) へ、settings の hook の一部を移す

起票日: 2026-10-02

> epic 618 の親 issue。判断の正本 (どれを移す / 移さない、前提、未確認の点) はここに置き、作業は同じディレクトリの子 issue で進める。

## 概要

Claude Code 2.1.287 に mods (関数 hook の plugin) が入った
(記事: https://claude.dev/blog/getting-started-with-claude-code-mods/ 、API の正本は plugin-authoring skill が書き出す `claude-code.d.ts`)。
settings の hook (`_claude/settings.json` の command) と比べて、次ができる:

- セッションに 1 回読み込まれて常駐し、状態をメモリと `$.state` / `$.store` に持てる (イベントごとに bash を起こさない)
- UI を描ける: プロンプトの上の帯 (`AbovePrompt`) / ペイン (`Pane`) / `$.ui.status` / `$.ui.toast`
- システムプロンプトに節を足せる (`prompt.compose`)。モデルが呼べるツール (`$.tool.register`) とスラッシュコマンド (`$.command.register`) を足せる
- settings の hook のイベントも `classic.<Event>` として受けられる。結果は settings の hook と同じ形 (`block` / `additionalContext` / PreToolUse の `allow`・`ask`・`deny`)。
  型定義の記述では、settings の hook が 1 本も無くても発火する

制約: モジュールは Node も DOM も無い隔離環境で動き、外の世界には `$` (fs / process / http / model …) 経由でしか届かない。
**API は early access で、リリースごとに変わる** (skill の reference.md が明記)。

## 判断: どの hook を移すか

settings に配線されている hook 16 本 (`_claude/hooks/*.sh`) を、mod にすると**何が新しくできるか**で分けた。
「移せるか」では分けない (`classic.*` があるので、型の上では全部移せる)。

| hook | 判断 | 子 issue |
|---|---|---|
| `issue-rules-inject.sh` (SessionStart で規約を注入、4 本) | **移す**。注入をシステムプロンプトの節へ上げる。今の `<system-reminder>` は CLAUDE.md と同じ拘束力を持たない (`CLAUDE.md` の「`_claude/` を触るとき」の節) | 620 |
| `human-tasks-due.sh` / `retro-open.sh` | **移す**。人へ見せる情報なので帯に出す。モデルに伝えさせるのをやめ、文脈を空ける | 621 |
| `tmux-pane-state.sh` (6 イベント。PostToolUse はツール呼び出しのたびに bash を起こす) | **移す**。状態を常駐側に持てる。速度の効果は未計測 (622 で before を測ってから) | 622 |
| `warn-discarding-checkout.sh` | **移す**。注入の警告を、消える差分を見せて進める / 止めるを選ぶペインに上げる | 623 |
| (新規) issue の採番 | **新設**。採番 → commit → push を 1 回のツール呼び出しにする | 624 |
| `ratelimit-warn.sh` / `next-claim-unshared.sh` | **今は移さない**。モデルに行動 (ユーザーへの提案 / push の伺い) をさせるための注入で、帯に出すだけでは役目を果たさない。621 の帯に同じ情報を足すかは 621 で決める | — |
| `deny-bare-tmux-kill.sh` / `deny-piped-push-then-destroy.sh` | **移さない**。守りの hook。mod の hook は失敗すると黙ってスキップされ、chain が続く (reference.md「Developing one」)。settings の hook のままにする | — |
| `issue-progress-check.sh` (Stop で差し戻す) | **今は移さない**。`classic.Stop` の `block` で移せるが、新しくできることが無い | — |
| `gofmt-on-edit.sh` / `git-state-verify.sh` / `next-claim-push.sh` / `normalize-settings.sh` / `claude-links-sync.sh` / `issue-progress-start.sh` | **移さない**。移しても新しくできることが無い。`claude-links-sync` と `normalize-settings` はセッションの外の状態 (link・settings ファイル) を直す仕事 | — |

**移さないと決めたものは、移植のコストを払う理由が出たとき (bash の起動が遅いという実測が出た、等) に見直す。**

## 前提と未確認の点 (619 で最初に潰す)

- [ ] **読み込まれる経路**。settings の hook は対話・`claude -p`・pro-con の PG (`--settings` を役割ごとに渡す。issue 431) のどれでも走る。
      mod は plugin として読み込まれる必要がある。reference.md によれば `CLAUDE_CODE_PLUGIN_DIRS` は、プロセスの環境か
      `~/.claude/settings.json` (= `_claude/settings.json` への link) の `env` から読まれる。**`claude -p` と PG でも読まれるかは未実測**
- [ ] **subagent / fork**。`prompt.compose` で足した節が subagent のシステムプロンプトにも入るかは未確認 (今の SessionStart の注入は subagent に届かない)
- [ ] **二重に効かないこと**。移行の途中で、mod と settings の hook が同じ注入・同じ表示をしないようにする (1 本ずつ切り替える)
- [ ] **API の変化への備え**。Claude Code の更新で mod が読み込まれなくなったとき、何が黙って止まるかを列挙しておく。守りの hook を移さないのはこのため

## 子 issue

- [ ] 619 — 土台: 置き場所・読み込みの配線・テスト・入口の文書 (**他の全部の前提**)
- [ ] 620 — issue 規約の注入をシステムプロンプトの節へ (最初の 1 本)
- [ ] 621 — human / retro の催促をプロンプトの上の帯へ
- [ ] 622 — tmux のペインの状態表示を mod へ
- [ ] 623 — 未コミットの変更を捨てる checkout の前に、確認のペインを出す
- [ ] 624 — issue の採番を、push まで済ませるツールにする

順番は 619 → 620 → (621 / 622 / 623 / 624 は独立)。

## 進捗

- 2026-10-02: 起票 (記事と `claude-code.d.ts` を読み、hook 16 本を分類した)
