# 914 (refactor): agents/ の重複を棚卸しし、手順・知識を skill へ移す

起票日: 2026-10-25

epic [910](../910-design-fold-subagents-into-main-loop.md)。forge のロスターに関わる部分は [913](../pending/913-design-forge-phase1-fold-investigation.md) の結論を待つ (913 は今の agent の本文をそのまま使って比べるので、913 がこの issue を待つことはない)。

## 概要

`_claude/agents/` には agent が 31 本ある (README を除く。2026-10-25)。記事は、チームや問題の種類ごとにサブエージェントを足していくと
重複が増え、標準と持ち主が曖昧になる (sprawl) と指摘している。手順・判断基準は skill (必要なときに読む) へ、
定型の実行はツールへ移し、サブエージェントは「独立した調査」と「ブラックボックスの検証」に限る、が記事の推奨。

### 実測 (2026-10-25)

利用回数: `~/.claude/projects` の直近 90 日の jsonl 5115 本で `"subagent_type":"<名前>"` を数えた (同じ呼び出しが親と子の transcript の両方に出る重複を含む概数):
general-purpose 2588 / Explore 140 / security-auditor 65 / code-reviewer 42 / go-architecture-designer 24 / rails-domain-designer 13 /
claude-code-guide 9 / architecture-reviewer 4。**dotfiles の agent 31 本のうち、上に出た 5 本 (security-auditor / code-reviewer / go-architecture-designer / rails-domain-designer / architecture-reviewer) 以外の 26 本は 0 回**
(forge.js から Workflow で起動した分は `agentType` に名前が入るはずだが、同じ期間で agent 名の `agentType` は 0 件。workflow の agent の残り方は未確認なので、forge 経由の利用は数えられていない)。
  → 2026-10-29: workflow の agent は `~/.claude/projects/<project>/<session>/subagents/workflows/<run>/agent-*.meta.json` の `agentType` に残る。下の「仕分け (2026-10-29)」はこれも数えた

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

## 仕分け (2026-10-29)

数えた範囲: このマシンの `~/.claude/projects` (transcript が残るのは 08-22 以降。起票時の「90 日・5115 本」は今は残っていない)。
「Agent 起動」は Agent ツールの `subagent_type`、「forge の run」は workflow の agent の `meta.json` の `agentType` (forge の run は 10-03 の 1 本・21 体)。
同じ数え方で、組み込みの general-purpose は 212 回、Explore は 59 回出る (数え方が効いていることの確認)。他のマシンの利用は数えていない。

| agent | Agent 起動 | forge の run | 仕分け | 理由 |
|---|---|---|---|---|
| appkit-swiftui-integration-expert | 0 | 0 | 残す (913 待ち) | `forge/_common/agents.md` の検出表が名指し (forge が状況で足す) |
| appstore-monetization-expert | 0 | 0 | 消した | 利用 0・dotfiles からの参照 0。ThumbnailThumb と baby-note が写しを持つ |
| appstore-submission-expert | 0 | 0 | 残す | `_claude/CLAUDE.md` のスキル参照の表が名指し |
| architecture-reviewer | 0 | 3 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約) |
| code-reviewer | 0 | 0 | 残す | cross-review の選定表が名指し |
| crash-analyzer | 0 | 0 | 残す | crash-log-analyzer skill が委譲先として名指し |
| css-expert | 0 | 0 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約) |
| data-persistence-expert | 0 | 0 | 残す (913 待ち) | `forge/_common/agents.md` の検出表が名指し (forge が状況で足す) |
| debugger | 0 | 0 | 残す (913 待ち) | forge の `phases-2-3.md` / `phases-5-completion.md` が名指し |
| dependency-analyzer | 0 | 1 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約)。description に MUST / PROACTIVELY |
| electron-expert | 0 | 0 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約) |
| go-architecture-designer | 0 | 0 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約)。description に MUST / PROACTIVELY |
| image-editing-expert | 0 | 0 | 残す (913 待ち) | `forge/_common/agents.md` の検出表が名指し (forge が状況で足す) |
| macos-system-integration-expert | 0 | 0 | 残す (913 待ち) | `forge/_common/agents.md` の検出表が名指し (forge が状況で足す) |
| nodejs-expert | 0 | 0 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約) |
| rails-domain-designer | 0 | 0 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約)。description に MUST / PROACTIVELY |
| refactoring-patterns | 0 | 0 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約)。description に MUST / PROACTIVELY |
| research-assistant | 0 | 1 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約) |
| security-auditor | 1 | 0 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約)。description に MUST / PROACTIVELY |
| smoke-test-runner | 0 | 0 | 消した | 利用 0・dotfiles からの参照 0。ThumbnailThumb が写しを持つ |
| swift-architecture-designer | 0 | 0 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約) |
| swift-concurrency-expert | 0 | 1 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約) |
| swift-language-expert | 0 | 4 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約) |
| swift-test-runner | 0 | 0 | 消した | 利用 0・dotfiles からの参照 0。写しはどこにも無い |
| swiftui-macos-designer | 0 | 2 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約) |
| swiftui-performance-expert | 0 | 2 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約) |
| swiftui-test-expert | 0 | 3 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約) |
| test-coverage-advisor | 0 | 1 | 残す (913 待ち) | forge の名簿 (`forge.js` / `_common/agents.md`) が名指し。913 の結論まで消さない (本文の規約)。description に MUST / PROACTIVELY |
| test-runner | 0 | 0 | 残す | forge の phases-5 (test-runner 連携) が名指し。description に MUST / PROACTIVELY |
| tt-api-expert | 0 | 0 | 消した | 利用 0・dotfiles からの参照 0。ThumbnailThumb が写しを持つ |
| xcodebuild-runner | 0 | 0 | 消した | 利用 0・dotfiles からの参照 0。ThumbnailThumb と baby-note が写しを持つ |

- **消した 5 本** (ユーザー決定 2026-10-29「5 本を消す」): forge の名簿の外で、利用 0・dotfiles (`_claude` / `tests` / `scripts` / `setup.sh` / `docs`) からの参照 0。
  ThumbnailThumb は `tt-api-expert` / `smoke-test-runner` / `xcodebuild-runner` / `appstore-monetization-expert` を、baby-note は後ろの 2 本を自分の `.claude/agents/` に持つ (中身は global と少し違う)。
  project の agent は同じ名前の global より優先されるので、その 2 repo には影響しない。`swift-test-runner` は写しが無く、名指しする所も無かった
- **913 待ちで残すもの**: forge の名簿と検出表・Phase のファイルが名指しする 22 本。本文の規約どおり、913 (保留中) の結論まで消さない。
  専門家の本文を skill (reference) へ移すかも 913 の結論 (案 B) 次第なので、ここでは仕分けない。913 を再開したら、この表から続ける
- **名指しされているので残すもの**: test-runner / code-reviewer / crash-analyzer / appstore-submission-expert (理由は表)
- **description に MUST / PROACTIVELY を持つもの**: 7 本 (表に記した)。`subagent-model-tiering.md` の「数回のツール呼び出しで終わる作業は委譲しない」とぶつかりうるが、
  このマシンでは名簿の外からの起動がほぼ 0 回 (security-auditor の 1 回だけ) なので、文言が起動を引っ張っている形は観測できなかった。文言は変えていない
- 消した agent の名前を forge.js に渡したときの挙動: 消した 5 本はどれも forge の名簿・検出表に無いので、渡る経路が無い (確かめていない)

## 進捗

- 2026-10-29: 本文を今のコードと突き合わせ、forge 経由の利用の数え方を足して、31 本を仕分けた (上)。ユーザーに諮って 5 本を消した。
  `_claude/agents/README.md` の索引 (31 → 26 件) を直し、`tests/claude/test_agents_index.sh` は OK (26 件 + 組み込み 1 件)。消した名前の参照は 0 件。
  名簿の 22 本は 913 待ちで残すので、913 を再開したときにこの表から続ける
