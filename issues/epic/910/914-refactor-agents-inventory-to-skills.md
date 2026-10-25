# 914 (refactor): agents/ の重複を棚卸しし、手順・知識を skill へ移す

起票日: 2026-10-25

epic [910](910-design-fold-subagents-into-main-loop.md)。forge のロスターに関わる部分は [913](913-design-forge-phase1-fold-investigation.md) の結論を待つ (913 は今の agent の本文をそのまま使って比べるので、913 がこの issue を待つことはない)。

## 概要

`_claude/agents/` には agent が 31 本ある (README を除く。2026-10-25)。記事は、チームや問題の種類ごとにサブエージェントを足していくと
重複が増え、標準と持ち主が曖昧になる (sprawl) と指摘している。手順・判断基準は skill (必要なときに読む) へ、
定型の実行はツールへ移し、サブエージェントは「独立した調査」と「ブラックボックスの検証」に限る、が記事の推奨。

### 実測 (2026-10-25)

利用回数: `~/.claude/projects` の直近 90 日の jsonl 5115 本で `"subagent_type":"<名前>"` を数えた (同じ呼び出しが親と子の transcript の両方に出る重複を含む概数):
general-purpose 2588 / Explore 140 / security-auditor 65 / code-reviewer 42 / go-architecture-designer 24 / rails-domain-designer 13 /
claude-code-guide 9 / architecture-reviewer 4。**dotfiles の agent 31 本のうち、上に出た 5 本 (security-auditor / code-reviewer / go-architecture-designer / rails-domain-designer / architecture-reviewer) 以外の 26 本は 0 回**
(forge.js から Workflow で起動した分は `agentType` に名前が入るはずだが、同じ期間で agent 名の `agentType` は 0 件。workflow の agent の残り方は未確認なので、forge 経由の利用は数えられていない)。

`_claude/skills` / `_claude/workflows` / `_claude/rules` / `_claude/CLAUDE.md` から名前で参照されていない agent (README からの参照は除く):
appstore-monetization-expert / smoke-test-runner / swift-test-runner / tt-api-expert / xcodebuild-runner。

重複が疑われる組 (名前と description からの推定で、**本文はまだ照合していない**):

- 設計: architecture-reviewer / swift-architecture-designer / go-architecture-designer / rails-domain-designer / refactoring-patterns / dependency-analyzer
- Swift の言語・UI: swift-language-expert / swift-concurrency-expert / swiftui-macos-designer / swiftui-performance-expert / appkit-swiftui-integration-expert
- テストの実行: test-runner / swift-test-runner / xcodebuild-runner / smoke-test-runner。実行と結果報告は「数回のツール呼び出しで自分が終わらせられる作業」で、
  `subagent-model-tiering.md` が委譲しない側に置いている
- ThumbnailThumb 専用: smoke-test-runner / tt-api-expert (dotfiles の global に置く必要があるか)

## タスク

- [ ] 31 本それぞれについて、利用回数・参照元 (skill / workflow / CLAUDE.md の表 / forge ロスター)・本文の中身 (手順か / 知識か / 観点か) を表にする。
      forge 経由の利用を数える方法を先に確かめる (913 の「未確認」と同じ論点)
- [ ] 表に「description が起動を強制する文言を持つか」の列を足す (go-architecture-designer / rails-domain-designer / refactoring-patterns などの description は
      「MUST be used」と書いている。`subagent-model-tiering.md` の「数回のツール呼び出しで終わる作業は委譲しない」と衝突し、利用回数そのものがこの文言に引っ張られている可能性がある)
- [ ] 各 agent を次のどれかに仕分ける:
  - **残す**: 独立した調査・ブラックボックスの検証として起動される実績があるもの (security-auditor / code-reviewer など)
  - **skill へ移す**: 本文が手順・領域知識で、main が読めば足りるもの (reference skill にする。例: Swift の言語・UI の知識)
  - **消す / 寄せる**: 実行だけの runner、参照も利用も無いもの、他と重複するもの
  - **その repo へ移す**: 特定のプロジェクト専用 (smoke-test-runner / tt-api-expert)
- [ ] 参照が 1 か所だけの agent は、その参照元も同じ変更で直す: test-runner (`_claude/skills/forge/phases-5-completion.md` の「test-runner 連携」) /
      image-editing-expert・macos-system-integration-expert (`forge/_common/agents.md` の検出表) / code-reviewer (`cross-review/SKILL.md` の選定表) /
      appstore-submission-expert (`_claude/CLAUDE.md` の表) / crash-analyzer (`crash-log-analyzer` skill が名指しで呼ぶ。skill 経由の起動が transcript の集計に出るかを先に確かめる)
- [ ] forge のロスター (`forge.js` の `resolveRoster` / `_common/agents.md`) が参照している agent は、913 の結論が出るまで消さない。
      cross-review の選定表 (`_claude/skills/cross-review/SKILL.md` の手順 3) の agent は 913 とは独立で、cross-review 自身の利用実績で判断する
- [ ] **消す・移す前にユーザーに確かめる**: 利用回数はこのマシンの transcript だけの値で、他の repo (obaket など) の CLAUDE.md・skill や他のマシンが agent を名指ししている可能性がある (未確認)。
      `~/.claude/CLAUDE.md` の表も css-expert / nodejs-expert / electron-expert を名指ししている (いずれも 0 回)。あわせて、消した agent の名前を forge.js に渡したときの挙動を 1 本で確かめる
- [ ] 消す・移すときは入口も同じ変更で直す: `_claude/agents/README.md` (索引。`tests/claude/test_agents_index.sh` が乖離を検出)、
      `~/.claude/CLAUDE.md` のスキルファイル参照の表 (agent の行)、`setup.sh` / `scripts/claude_links.sh` の link (消した agent の古い link は `./setup.sh` が掃除する)
- [ ] 仕分けの表と理由をこの issue に残す (消したものの再提起を止めるため)

## 受け入れ条件

- 31 本すべてに仕分けと理由がある
- 消した・移した agent の参照が 0 件 (`grep -rn '<名前>' _claude ~/.claude/CLAUDE.md`)、`make test` が通る

## 関連ファイル

- `_claude/agents/` / `_claude/agents/README.md` / `tests/claude/test_agents_index.sh`
- `_claude/workflows/forge.js` / `_claude/skills/forge/_common/agents.md` / `_claude/skills/cross-review/SKILL.md`
- `_claude/rules/subagent-model-tiering.md`

## 進捗

起票のみ。
