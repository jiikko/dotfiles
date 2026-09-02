# 165 docs: プロンプト監査 (`/claude-api prompt-audit`) の結果と提案 diff (2026-09-02)

起票日: 2026-09-02

`/claude-api prompt-audit` を dotfiles の Claude 設定 (`_claude/`) 全体に回した結果。監査の契約は
「レポート + 提案 diff を出して止まる」なので、**この issue に書いた変更はどれもまだ当てていない**。
hunk 単位で採否を決めてから当てる。

⚠️ **2026-09-02 に diff 1 / 2 / 3 (H1 / H2 / H3) は適用済み**。最新の状態は末尾の「適用ログ」を見る
(下の「提案 diff」節は起票時のままなので、適用済み hunk の `-` 行はもう原文と一致しない)。

## 前提 (監査が置いた仮定)

- **スコープ**: リクエストにファイル指定が無いので、working directory 全体のプロンプト面。
  `_claude/CLAUDE.md` (156 行) / `_claude/rules/*.md` (35 本・2,178 行・171 KB、**毎セッション全文ロード**) /
  `_claude/skills/**/*.md` (17 skill・約 5,900 行) / `_claude/agents/*.md` (31 本・9,702 行) /
  `_claude/_common/*.md` / `_claude/commands/fork-scratch.md` / hook が注入する文言 /
  各 `CLAUDE.md` (root・tests・scripts・src/glogx・_claude/workflows) / `.claude/rules/`
- **ターゲットモデル**: Claude Fable 5.1。根拠は `_claude/settings.json` の `"model": "fable"` と、
  このセッション自身が `claude-fable-5-1` で動いていること。サブエージェントは Opus 5 / Sonnet 5 前提
- 非 Anthropic provider のマーカー (openai 等) はプロンプト面に無い
- 一次スキャンは agents / skills を read-only の sonnet サブエージェント 2 体に分担させ、
  High / Medium の全件は main が実ファイル・実コマンドで裏を取った (下記「検証済み」の記述はその結果)

## 全体の結論

**英語圏のプロンプト監査で典型的な劣化 (CAPS の圧力語・「think step by step」・退役モデルの workaround・更新抑制・
反フォーマット規則・グレーダー語彙) はほぼゼロ**。grep での該当は agents の英語テンプレ数箇所だけで、
日本語本文の「必ず / 禁止」はほぼ全件に理由が併記されていた。

見つかったのは別の 3 系統:

1. **事実が腐ったピン** (High): 退役した codex フラグの注記、`forge` の spec 文書と `forge.js` の食い違い、
   `fable` skill が memory と逆のことを書いている、`Opus 5` 固定のトレイト記述
2. **本文に埋まった事故ナラティブ** (Medium): repo 規約 (root `CLAUDE.md`「本文は規範だけにし、なぜ・起源・実例は
   `rules-rationale/` に置く」) に反して rules 本文に残る実例。rules は毎セッション 171 KB ロードされるので
   ここだけがセッション単価に効く
3. **agents の英語テンプレ由来の定型** (High〜Medium): 「Surface-level X knowledge is insufficient」「am I being lazy?」
   「Be Proactive / Be Specific」、同じ主張の 4 回繰り返し、絵文字進捗テンプレ

件数: High 8 / Medium 13 / Low (flag のみ) 6。

## 検証済みの事実 (レポートの根拠)

| 主張 | 確認方法 |
|---|---|
| `--full-auto` は codex-cli 0.152.1 に存在しない | `codex --version` = 0.152.1。`codex exec --help` / `codex --help` に `full-auto` の文字列なし |
| `modes.md:46` は実装と食い違う | `_claude/workflows/forge.js:247-257` `resolveRoster()`: Maximum/Ultra が無条件に push するのは `dependency-analyzer` と `test-coverage-advisor` の 2 つ。`refactoring-patterns` は `extra` 経由 (`agents.md:389` の「リファクタ系タスクのみ」と一致) |
| `agents.md` の per-agent プロンプトは死んでいる | `agents.md:127` 自身が「実行時には使われない…編集しても動作は変わらない」と明記 |
| `fable/SKILL.md:17` は memory と逆 | memory `no-autonomous-commit-push`: 「commit は適宜自律 OK (2026-07-17〜)。push も都度自律 (2026-08-29〜)」 |
| `Opus 5` 記述の起源 | `git blame`: `_claude/CLAUDE.md:25,33` と `subagent-model-tiering.md:11` はいずれも `b1bee88c` (2026-07-27) |
| rules-rationale の欠落 | 35 rules 中 7 本に同名の rationale ファイルが無い: avoid-wall-clock-assertions / claude-md-layer-prompt / claude-md-maintenance / no-concurrent-spm-build-during-xcodebuild / no-ios-simulator-verification / pending-issue-rationale-in-code / refuse-low-value-coverage |
| 腐っていなかったもの | rules / CLAUDE.md が指す repo 内パスは全件実在 (issue 098 / 100 は `issues/done/`)。forge の `Phase -1` / モード名は `forge/SKILL.md:56-62` に実在。`codex exec review` の使用フラグ (`--uncommitted` `--base` `-s` `--ephemeral` `-o` `-c`) は 0.152.1 の help に全部ある。simctl のデバイス名 (`Apple Watch Series 10 (46mm)` / `iPhone 17`) は有効 |

## 所見 (信頼度順)

### High

| # | 場所 | 証拠 | パターン | なぜ今は不要か | 提案 |
|---|---|---|---|---|---|
| H1 | `_claude/skills/codex-review/SKILL.md:205` | 「`--full-auto` は付けない（codex-cli 0.139.0 時点で `--sandbox workspace-write` の deprecated alias であり…alias が削除されたら本注記ごと整理してよい）」 | 1d fossil (バージョン固定の workaround) | 0.152.1 でフラグ自体が消え、注記が自ら書いた整理条件が成立した。`codex-lead/SKILL.md:111,205,260` と `codex-drive/SKILL.md:524,690,719,1036` はこの行を「正本」として参照しているので、直すのはここ 1 箇所 | rewrite (diff 1) |
| H2 | `_claude/skills/forge/_common/modes.md:46` | 「**追加エージェント**: dependency-analyzer, test-coverage-advisor, refactoring-patterns」(Maximum モード) | Group 2 (重複が食い違う) | `agents.md:389` と `forge.js` は refactoring-patterns をリファクタ系タスク限定にしている。modes.md だけ読むと常時起動と誤読する | rewrite (diff 2) |
| H3 | `_claude/skills/forge/_common/agents.md:125-294, 339-417` | 「⚠️ 以下の per-agent「Phase 1/4 用プロンプト」は実行時には使われない（履歴・参考）…ここのプロンプトを編集しても動作は変わらない」 | Group 2 (自認している死んだスキャフォールド) | 約 280 行のプロンプト本文が、この後ろにある実際に読まれる表 (条件付き必須 / 追加 / 言語別置換) への到達を遅らせるだけ | remove: 各 `### N. <agent>` の見出しと 1 行の役割だけ残し、```` ``` ```` で囲まれたプロンプト本文を全部消す。履歴が要るなら `git log` で足りる (diff 3 は範囲指定) |
| H4 | `_claude/skills/fable/SKILL.md:17` | 「**commit / push はユーザー本人の操作** (memory: no-autonomous-commit-push)」 | Group 2 (重複が食い違う) | 引用先の memory は現在「commit は適宜自律 OK / push も都度自律」。skill が memory の名前を借りて逆の規範を再導入している | rewrite (diff 4) |
| H5 | `_claude/skills/fable/SKILL.md:25-27` | 「Claude 5 ファミリー第 1 号…(model id: `claude-fable-5`)」「knowledge cutoff: 2026-01」 | Group 2 (モデル名ピン) | 実行モデルは `claude-fable-5-1` (cutoff 2026-06)。skill 自身が「乖離に気づいたら同じ commit で更新」と書いている。また skill の前提「エミュレート側は Opus 5」は、`settings.json` が fable を指す今は逆 (Fable 本人が Fable エミュ規範を読む) | rewrite (diff 5)。「Fable 本人が読んだときは仕様節を飛ばして規範節だけ効かせる」一文を足す |
| H6 | `_claude/agents/research-assistant.md:213,215` | 「"Have I been thorough enough, or am I being lazy?"」「Remember: Your research directly impacts…」 | 1a (do-not-be-lazy) / 1c (Remember, 系の padding) | 現行モデルは既定で proactive。lazy 系の自問は過剰再検証を誘発する | remove (diff 6) |
| H7 | `_claude/agents/{css-expert:12, electron-expert:12, nodejs-expert:12, swift-language-expert:14, research-assistant:11}` | 「**Surface-level X knowledge is insufficient.** You must demonstrate:」+ 一般的美徳の箇条書き | 1a 圧力 + 1c 一般的美徳 (4 ファイルは同一テンプレ、research-assistant は「Surface-level answers are unacceptable.」の同型変種) | 「深い知識を示せ」は訓練済みの既定で、指示として読まれると出力が長く防御的になる。後続の箇条書きは対象領域の列挙としては使えるので見出しだけ変える | rewrite (diff 7) |
| H8 | `_claude/agents/smoke-test-runner.md:45,53` | 「これを怠ると、どの操作で問題が発生したか特定できない」が 2 回 | 1c 反復 | 同文の反復 | remove 片方 (diff 8) |

### Medium

| # | 場所 | 証拠 | パターン | なぜ | 提案 |
|---|---|---|---|---|---|
| M1 | `_claude/CLAUDE.md:25,33` / `_claude/rules/subagent-model-tiering.md:11` | 「Opus 5 は既定で「よく喋り・よく書き・スコープを広げ・よく委譲する」方向に寄る」「Opus 5 は指示なしでも自分の作業を見直すため」「Opus 5 は以前のモデルより積極的にサブエージェントへ委譲する」 | Group 2 (モデル名ピン) + 1a トレイト主張 | 実行モデルは Fable 5.1。規範 (本題に紙面を使う / 自己再チェックを足さない / 小タスクは委譲しない) はモデル非依存で有効なので、**規範は残しモデル名だけ外す**。Fable 5.1 は逆に under-narrate 側なので「進捗更新のペース」行 (:29) は「いつ書くか」を言う形になっており、そのまま残す | rewrite (diff 9, 10) |
| M2 | `_claude/CLAUDE.md:123` | 「重要なコード変更・バグ修正は、設計と実装の両方を codex レビューに通すことを基本とする」 | Group 2 (重複が食い違う) | memory `no-codex-usage` (2026-07-17 / 08-13 の指示) は「codex は自発的に起動しない。CLAUDE.md より優先」。毎セッション両方を読んで相殺している。`issue-creation-codex-review.md:13` には既に代替経路 (観点分割サブエージェント) が書かれている | rewrite (diff 11): 「外部レビュー」を主語にし、codex は許可された環境での選択肢に下げる |
| M3 | `_claude/rules/*.md` 本文の事故ナラティブ | `adversarial-review-own-safeguards.md:30-37,115-118` / `mutation-verify-new-tests.md:119-123` / `verify-execution-not-just-exit-code.md:64-67,71` / `move-report-conclusions-to-issues.md:47-53` / `refuse-low-value-coverage.md:58-62` / `parallel-write-agents-need-worktree-isolation.md:19-28` / `avoid-wall-clock-assertions.md:22-35` | Group 2 (history narratives) + repo 規約違反 | root `CLAUDE.md` が「本文は規範だけ、実例は rules-rationale へ」と定めている。rules は毎セッション 171 KB 読まれ、実例段落はその 1 割強。**規範文と「なぜ」の 1 行は残す** (keep list: 理由は cruft ではない)。移す先の rationale ファイルが無い 7 本は新設が要る | move: 各ブロックを `_claude/rules-rationale/<同名>.md` へ。本文には「実例: rationale 参照」の 1 行 (diff 12 は代表 1 件) |
| M4 | `_claude/skills/codex-drive/SKILL.md` (例 `:342-350, 527-539, 546-552`) | 「実測根拠 (2026-08-28 swift-smbee…): D1 263k / D3 359k…」等、日付・issue 番号・トークン数つきの事故記述が十数箇所 | Group 2 (history narratives) | 1,040 行の skill を読むたびに払う。指示 (`effort` 固定 / 出力は run 固有パス) は背景無しで従える。ただし `perf-claims-need-measurement.md` が要求する「実測を残す」との緊張があり、意図的な選択の可能性がある | move (rationale 形式の付録へ) か、意図的なら現状維持を SKILL.md 冒頭に 1 行明記 |
| M5 | `_claude/skills/forge/_common/cross-review.md:71-308` / `_claude/skills/forge/phase-4.3-ultra.md:27-115` / `_claude/skills/forge/examples-*.md` | 出力形式を絵文字見出しの markdown (`✅ / ⚠️ / ❌ / 💡`, `🆕 / 🔄 / 🔍`) で描く | Group 2 (spec と実装のずれ) | `forge.js` は `CROSS_VERDICT_SCHEMA` 等の JSON スキーマで出力を縛る。概念は一致し、cross-review.md 冒頭は「skill 層は直接読まなくてよい」と書くので実害は小さいが、`agents.md:127` にある「編集しても動作は変わらない」相当の断り書きが無い | add: 冒頭バナーに「以下の出力例は説明用。実際の wire format は forge.js の `*_SCHEMA`」の 1 行 |
| M6 | `_claude/agents/test-runner.md:53-62, 71-84` | 「You MUST NEVER do the following to make tests pass:」+ 7 項目 / 絵文字つき進捗テンプレ (`🔍 Identifying… 🏃 Running… 🎉 All tests passing!`) | 1a 圧力 (禁止項目自体は実在する failure なので**残す**) / 1f 出力整形コレオグラフィ | 禁止リストは「テストを弱めて green にする」という現行モデルでも起きる失敗への防御で keep list 5 に該当。直すのは register と、進捗テンプレ (現行モデルは適切に報告する。固定テンプレは出力の型を凍結する) | rewrite (diff 13) |
| M7 | `_claude/agents/debugger.md:10-17, 19-25, 85-91, 143-152` | 「The symptom is never the cause」系の同一主張が Core Philosophy / Core Principles / Anti-Patterns / Quality Gates の 4 節で反復 | 1c 反復 | 同じ規則の 4 通りの言い換えを reconcile するコストだけ増える | rewrite: 1 節 (Core Principles) に統合し、Anti-Patterns は「理由つきの 3 項目」に圧縮 |
| M8 | `_claude/agents/research-assistant.md:29-67, 184-197` | 質問種別ごとの「Search 1 / Search 2 / … Search 5」固定手順 ×4 / 「Never… / Always…」5 連 | 1c コレオグラフィ + 1e 出自なしの禁止クラスタ | 検索計画は判断タスクで、現行モデルの自前計画の方が入力に適応する。Never/Always 列は理由が無い | rewrite: 手順を「出典の優先順位 + 打ち切り条件」の散文に、禁止列を理由つき 2 項目に |
| M9 | `_claude/agents/go-architecture-designer.md` ↔ `swift-architecture-designer.md` | `:8` の役割文が言語名以外同一。「Hard Rules (Breaking these = Design Failure)」「Finishing: 3 Weaknesses」ブロックが共通 | Group 4 (近似重複エージェント) | 言語という実在の差はあるので統合はしない。共通スキャフォールドは `_common/` に 1 部 | move: 共通部を `_claude/_common/architecture-designer-scaffold.md` へ (要: agent ファイルからの参照が実際に展開されるか確認。L2 参照) |
| M10 | `_claude/agents/data-persistence-expert.md:316` / `swiftui-test-expert.md:354` | 「(Limited support as of 2025)」/ 検索例 「"XCUITest flaky test fix 2024"」 | Group 2 (時間依存の記述) | 年が固定されている。`research-assistant.md` は同じ用途に `[current year]` を使っている | rewrite (diff 14) |
| M11 | `_claude/agents/{appstore-monetization-expert:373-378, rails-domain-designer:104-107, swiftui-macos-designer:233-236}` | 「## Working Style — 1. Be Strategic 2. Be Compliant …」 | 1c 一般的美徳 | 訓練済み既定の再掲。「Be Compliant: Always check against latest App Store Review Guidelines」のような**契約を含む項目は残す** | rewrite: 美徳項目を落とし、契約を含む項目だけ本文に残す |
| M12 | `_claude/commands/fork-scratch.md:17-44` | 「次を **そのまま** 実行すること」以下、`exit 0` ガード (:21) の後ろに到達不能な手順・実行後の案内 約 20 行 | 1d (到達不能な指示) | ガードで常に終了するのでこの 25 行は実行されない。復活しない判断済み (`docs/claude-fork-popup.md`) | rewrite (diff 15): ガードと docs へのポインタだけにする |
| M13 | `_claude/rules-rationale/` の欠落 7 本 | 上表「検証済みの事実」参照 | Group 2 (規約と実体のずれ) | root `CLAUDE.md` の規約は rules 全件に rationale があることを前提にしている。M3 の移送先でもある | add: 7 本を新設 (M3 と同じ commit) |

### Low (flag のみ。diff は出さない)

- **L1 Group 2 汎用知識の量**: agents 15 本が 150〜570 行ずつ教科書的解説 (CSS cascade / Node.js event loop / SwiftData / macOS API / Fowler のカタログ…) を持つ (汎用解説の推定行数 / 総行数: `nodejs-expert.md` 約 570 / 702、`css-expert.md` 約 450 / 624、`swift-language-expert.md` 約 420 / 571)。skills では `watchos-expert/` 全体 (自ら「Apple 公式ドキュメント基準」と書く) と `ios-app-developer/topics/swiftui-ios.md`、`style-review/SKILL.md:26-59` (WCAG の数値)、`ux-visibility-review/SKILL.md:150-173` (HSL / ΔE の定義)。起動時にしか読まれないので単価は M3 より低い。削るかは「サブエージェントの出力が実際に浅いか」で判断する話で、監査からは flag に留める
- **L2 `## Language Adaptation` / `## Tool Selection Strategy` のインライン重複**: 23 ファイルが同名の節を持つ。Language Adaptation 側は `_common/` と同内容 (差分は例示の 1 行程度で**食い違っていない**)。Tool Selection Strategy 側は見出しだけ共通で、中身は agent 固有の箇条書き (`_common/` の表 + フローチャートとは別物) なので重複ではない。keep list 8 (機能している冗長) に該当し cruft ではない。参照形式 `@../_common/...` を使うのは `architecture-reviewer.md` と `swift-language-expert.md` の 2 本だけで、**agent ファイル内の `@` 参照が実際に展開されるかは未確認**。展開されるなら統合、されないならインラインが正で、参照している 2 本の方が壊れている
- **L3 `_claude/CLAUDE.md`「スキルファイル参照」表**: 17 skill と 1:1 で一致し、記述も skill の `description` と食い違わない。Skill ツールが description を自動で提示する今は重複だが、機能している冗長
- **L4 root `CLAUDE.md:5-6`**: 「移行は完了済み (issue 133)…外した」の経緯記述。1d の migration-relative phrasing に形は合うが、repo 規約は CLAUDE.md に「Why」を残す方針なので意図的
- **L5 `test-runner.md` ↔ `swift-test-runner.md` / `swiftui-test-expert.md:405-430`**: ThumbnailThumb の `make test-debug` 手順が 3 ファイルに重複。内容は一致。統合は好みの問題
- **L6 agents 7 本が参照する第三者 skill パッケージ (`@rshankras` / `@dimillian` / `@jamesrochabrun` / `@swift-skill`)**: 実在・現行版かは未確認

## 提案 diff (hunk 単位で採否を決める。未適用)

### diff 1 — H1 `_claude/skills/codex-review/SKILL.md:205` ✅ 適用済み (`3b272e9`)

```diff
-- 常に `--ephemeral -o "$review_out"` を付与する。`--full-auto` は付けない（codex-cli 0.139.0 時点で `--sandbox workspace-write` の deprecated alias であり、`-s read-only` と併用すると後勝ちで上書きして codex が書き込み可能になることを実測確認済み。codex 側で alias が削除されたら本注記ごと整理してよい）
+- 常に `--ephemeral -o "$review_out"` を付与する。sandbox は `-s read-only` を明示する（`--full-auto` は codex-cli 0.152.1 で削除済み。渡すと未知の引数として即エラー）
```

### diff 2 — H2 `_claude/skills/forge/_common/modes.md:46` ✅ 適用済み (`3b272e9`)

```diff
-- **追加エージェント**: dependency-analyzer, test-coverage-advisor, refactoring-patterns
+- **追加エージェント**: dependency-analyzer, test-coverage-advisor（無条件）。refactoring-patterns はリファクタ系タスクのときだけ（判定は agents.md「Maximum 専用エージェント」）
```

### diff 3 — H3 `_claude/skills/forge/_common/agents.md` ✅ 適用済み (`788122b`)

範囲指定。`## 必須エージェント（6+1つ）` (:125) 〜 `## 条件付き必須エージェント` (:295) の直前と、
`## Maximum 専用エージェント` (:339) 〜 `## 言語別エージェント置換ルール` (:418) の直前の各 `### N. <agent>` 配下から、
```` ``` ```` で囲まれた「Phase 1（事前調査）用プロンプト」「Phase 4（レビュー）用プロンプト」のブロックを削除。
残すのは見出し・`(model: …)`・「条件:」行・1 行の役割説明。:127 の ⚠️ 段落は「プロンプト本文は forge.js の
`investigatePrompt()` / `reviewPrompt()` が生成する」の 1 文に縮める。

### diff 4 — H4 `_claude/skills/fable/SKILL.md:17`

```diff
-- **commit / push はユーザー本人の操作** (memory: no-autonomous-commit-push)。下記の自走規範を「commit はワークフローの延長」と解釈して上書きしない
+- **commit / push の可否は memory (no-autonomous-commit-push) が正本**で、本 skill は上書きしない。現在の内容は「検証 green の作業単位ごとに pathspec で commit し、都度 push」。下記の自走規範を、memory が禁じている操作まで広げる根拠にしない
```

### diff 5 — H5 `_claude/skills/fable/SKILL.md:20-27`

```diff
 ## Fable 5 の仕様
 
-出典は 2 系統 (いずれも 2026-07 時点)。モデル体系・呼称は変わりうるので、乖離に気づいたらこの節を同じ commit で更新すること (claude-md-maintenance.md の「触ったら直す」)。
+出典は 2 系統 (2026-09 時点)。モデル体系・呼称は変わりうるので、乖離に気づいたらこの節を同じ commit で更新すること (claude-md-maintenance.md の「触ったら直す」)。
+**実行モデルが Fable 系のとき (`settings.json` の `model: fable`) はこの節と「エミュレート側の事情」段落は読み飛ばし、「行動規範」だけを効かせる** — 本 skill は他モデルで Fable の働き方を再現するためのもので、Fable 本人には仕様の説明は要らない。
 
 - Fable 本人のシステムプロンプトの写し:
-  - Anthropic の Claude 5 ファミリー第 1 号。Opus より上位の Mythos クラスに属する (model id: `claude-fable-5`)
-  - Claude Mythos 5 と同一の基盤モデル。Fable はデュアルユース能力への追加安全策込みの一般提供版、Mythos は承認組織限定版 (https://www.anthropic.com/news/claude-fable-5-mythos-5)
-  - knowledge cutoff: 2026-01
+  - Claude 5 ファミリー。Opus より上位の Mythos クラスに属する (現行 model id: `claude-fable-5-1`。前世代 `claude-fable-5` も提供中)
+  - Claude Mythos 5.1 と同一の基盤モデル。Fable はデュアルユース能力への追加安全策込みの一般提供版、Mythos は承認組織限定版 (https://www.anthropic.com/claude/fable)
+  - knowledge cutoff: 2026-06 (5.1)
```

### diff 6 — H6 `_claude/agents/research-assistant.md:213-215`

```diff
 4. "Does my recommendation actually fit the user's context?"
-5. "Have I been thorough enough, or am I being lazy?"
-
-Remember: Your research directly impacts code quality and developer productivity. Shallow research leads to technical debt. Deep research prevents problems before they occur.
```

### diff 7 — H7 5 ファイル共通 (例: `_claude/agents/css-expert.md:10-12`。他 4 本も同型)

```diff
-## Core Philosophy: Deep CSS Expertise
-
-**Surface-level CSS knowledge is insufficient.** You must demonstrate:
+## Focus areas
+
```

`electron-expert.md:10-12` / `nodejs-expert.md:10-12` / `swift-language-expert.md:12-14` / `research-assistant.md:9-11` も同じ置換
(見出しを「Focus areas」に、太字の 1 文を削除。後続の箇条書きは対象領域の列挙として残す)。

### diff 8 — H8 `_claude/agents/smoke-test-runner.md:53`

```diff
-これを怠ると、どの操作で問題が発生したか特定できない。
```
(:45 の同文を残す)

### diff 9 — M1 `_claude/CLAUDE.md:25`

```diff
-Opus 5 は既定で「よく喋り・よく書き・スコープを広げ・よく委譲する」方向に寄る（reasoning effort は思考量を制御するだけで、目に見える出力の長さは制御しない）。ここは明示指示でしか効かないので言語化しておく。出典: [公式ガイド](https://platform.claude.com/docs/ja/build-with-claude/prompt-engineering/prompting-claude-opus-5)（乖離に気づいたら同じ commit で直す）。
+出力の長さ・スコープ・委譲の量は reasoning effort では制御できず、明示指示でしか効かないので言語化しておく（モデルによって寄る方向は違う: Opus 5 は多く喋り・広げ・委譲する側、Fable 5.1 は逆に報告が少なくなる側。出典: [Opus 5 ガイド](https://platform.claude.com/docs/ja/build-with-claude/prompt-engineering/prompting-claude-opus-5) / `claude-api` skill の Fable 5.1 移行節）。
```

### diff 10 — M1 `_claude/CLAUDE.md:33` と `_claude/rules/subagent-model-tiering.md:11`

```diff
-- **自分の判断で自己再チェックの手順を足さない**。Opus 5 は指示なしでも自分の作業を見直すため、その場の思いつきで「もう一度読み返すパス」「確認用サブエージェント」を積むのはトークンを増やすだけで品質を上げない。
+- **自分の判断で自己再チェックの手順を足さない**。現行モデルは指示なしでも自分の作業を見直すため、その場の思いつきで「もう一度読み返すパス」「確認用サブエージェント」を積むのはトークンを増やすだけで品質を上げない。
```

```diff
-Opus 5 は以前のモデルより積極的にサブエージェントへ委譲する。委譲が効くのは「本当に独立していて並列化できる大きな作業」だけで、小さなタスクに適用するとコストと所要時間が倍になる。
+委譲が効くのは「本当に独立していて並列化できる大きな作業」だけで、小さなタスクに適用するとコストと所要時間が倍になる（現行モデルは指示なしでも積極的に委譲するので、抑える側の指示が要る）。
```

### diff 11 — M2 `_claude/CLAUDE.md:123`

```diff
-- **重要なコード変更・バグ修正は、設計と実装の両方を codex レビューに通すことを基本とする**（設計 → codex レビュー → 実装 → テスト → codex レビュー）。codex の指摘は無視せず、根拠の弱い断定・false positive を訂正してから commit する
+- **重要なコード変更・バグ修正は、設計と実装の両方を外部レビューに通すことを基本とする**（設計 → レビュー → 実装 → テスト → レビュー）。レビュワーは、codex の使用がユーザーに許可されている環境では codex、それ以外では観点を分けた read-only サブエージェント（作法は [`issue-creation-codex-review.md`](rules/issue-creation-codex-review.md) の代替節）。指摘は無視せず、根拠の弱い断定・false positive を訂正してから commit する
```

### diff 12 — M3 代表 1 件 `_claude/rules/adversarial-review-own-safeguards.md:30-34`

```diff
 - 問い: 「この残骸は**どこで生まれるか**。生まれる側を差し替えれば掃除は要らないのでは?」
-- 実例 (obaket 688 C7, 2026-09-01): E2E が実 Keychain に残す credential の残骸に対し、
-  当初は「E2E 形状の service を消して回る sweep」を実装しかけた。ユーザー指摘で
-  **E2E profile の永続化層自体を in-memory 実装へ差し替える**方針に変えたところ、
-  破壊的操作を作らずに済み、変更は composition root の分岐 1 箇所で終わった
-  (差し替え先は既に `any CredentialRepository` で保持されていた)
+- 実例は rationale (obaket 688 C7: sweep を作る代わりに永続化層を in-memory に差し替え、破壊的操作ゼロで済んだ)
```
削った 5 行は `_claude/rules-rationale/adversarial-review-own-safeguards.md` へそのまま移す。M3 の他 6 箇所も同型。

### diff 13 — M6 `_claude/agents/test-runner.md:53-62, 71-84`

```diff
-## Strict Prohibitions
-
-You MUST NEVER do the following to make tests pass:
+## Do not make tests pass by weakening them
+
+A green suite obtained this way hides the regression the test was written to catch. Specifically, do not:
 - Skip or disable failing tests (`@skip`, `.skip`, `xit`, etc.)
 ...(7 項目はそのまま)
```

```diff
 ## Output Format
 
-Provide clear status updates:
-```
-🔍 Identifying relevant tests for: [changed files/modules]
-📋 Tests to run: [list of test files/patterns]
-🏃 Running tests...
-❌ Failures found: [count]
-🔧 Analyzing failure: [test name]
-   Root cause: [explanation]
-   Fix: [description of product code fix]
-✅ Re-running tests...
-🎉 All tests passing!
-```
+Report: which tests ran (and how they were selected), each failure with its root cause and the product-code fix applied, and the result of the re-run. Quote runner output for failures rather than paraphrasing it.
```

### diff 14 — M10

```diff
--- a/_claude/agents/data-persistence-expert.md
-**SwiftData Migration** (Limited support as of 2025):
+**SwiftData Migration** (check the current release notes — support has been expanding release by release):
--- a/_claude/agents/swiftui-test-expert.md
-   - "XCUITest flaky test fix 2024"
+   - "XCUITest flaky test fix [current year]"
```

### diff 15 — M12 `_claude/commands/fork-scratch.md:17-44`

:17 以降を次の 6 行に置き換える (frontmatter と :6-15 のバナーは残す):

```markdown
次を実行し、その出力をユーザーに伝える:

```bash
echo "/fork-scratch は一時無効化中です (休眠。理由と復活手順は docs/claude-fork-popup.md)。"
```

復活させるときの手順 (fork コマンド本体・`C-t b` の popup・失敗時の点検) は `docs/claude-fork-popup.md` を正本にする。
```

## 適用するときの注意

- **1 hunk = 1 commit** に分けると効果が帰属できる (監査の Step 6)
- M3 / M13 は同じ commit (rationale 新設と本文の移送)。移した後 `make test` (`tests/claude/`) を回す
- H1 を当てたら `codex-lead` / `codex-drive` の参照 7 箇所が「正本」として指す文が変わる。文言は「付けない」のままで整合するので追従不要だが、`grep -rn full-auto _claude/skills` で目視する
- Step 7 (削除は仮説): M1 / M2 / M6 のように振る舞いに触る hunk は、当てた次のセッションで「報告の量・レビューの起動先・テスト報告の形」が意図どおりかを見る。戻すなら簡潔な形で再挿入する (元の冗長な文を復元しない)

## 却下 / 対象外と判断したもの (次の監査が同じ指摘を出さないため)

- 日本語の「必ず / 禁止 / 絶対」(rules 全体で数十件): 全件に理由か強制手段が併記されており、1a の「理由の無い圧力」に当たらない
- `test-runner.md` の禁止 7 項目そのもの: テストを弱めて green にする失敗は現行モデルでも起きる (keep list 5)。直すのは register だけ (M6)
- `_claude/CLAUDE.md:29` 進捗更新のペース: 「いつ書くか」を言う形で、Fable 5.1 向けに推奨される置換後の形になっている
- hook (`human-tasks-due.sh` / `retro-open.sh` / `git-state-verify.sh`) が注入する文言: データの提示で、行動を steer していない
- `tests/CLAUDE.md` / `scripts/CLAUDE.md` / `src/glogx/CLAUDE.md`: 環境事実と制約の理由 (keep list 1)。日付つきの短い出典は規約どおり
- `no-osascript-for-ui-verification.md` の `scripts/verify.rb` / root `CLAUDE.md` の `scripts/check_platform_dialect.sh`: 前者は例示、後者は「外した」と明記された過去の道具で、どちらも腐ったポインタではない
- `escalate-to-forge-after-failed-tries.md:5` 「forge v2 は Phase -1 の確認を省略」: `forge/SKILL.md:56-62` に実在する挙動

## 反証レビューの結果 (2026-09-02, read-only サブエージェント 1 体)

P1 (事実誤認 / hunk が当たらない) は 0 件。`-` 行は全 hunk が原文と一致。P2 として訂正したもの: High の件数 (7 → 8)、M5 の 3 ファイルの置き場所 (`_common/` は cross-review.md だけ)、H7 の「5 ファイル同一」(4 同一 + 1 同型)、L1 の行数 (推定の汎用行数を総行数のように読める書き方だった)、L2 の Tool Selection Strategy 側 (見出しだけ共通で中身は agent 固有。重複ではない)。diff 3 の境界を実見出し行 (:295 / :418) に合わせた。

## 適用ログ (2026-09-02、途中まで)

1 hunk = 1 commit で直列に適用中。ユーザーの指示でここで一旦停止。

| hunk | commit | 内容 |
|---|---|---|
| diff 1 (H1) | `3b272e9` | codex-review SKILL.md の `--full-auto` 注記を 0.152.1 の現状へ (削除済みフラグ)。実測: `codex --version` = 0.152.1 / `codex exec --help` に該当 0 件 |
| diff 2 (H2) | `3b272e9` | modes.md の Maximum ロスターに refactoring-patterns の条件を明記 |
| diff 3 (H3) | `788122b` | agents.md の per-agent プロンプト本文を削除 (281 → 233 行)。見出し・条件行は残し、役割 1 行に圧縮。冒頭 ⚠️ も 1 文へ |

### 未適用 (残り)

- **High**: H4 / H5 (fable SKILL.md)、H6 / H7 / H8 (agents の英語テンプレ)
- **Medium**: M1 (diff 9, 10)、M2 (diff 11)、M3 + M13 (rules 本文の実例を rationale へ / 欠落 7 本の新設)、
  M4・M5・M7・M8・M9・M10 (diff 14)・M11・M12 (diff 15)
- 適用後の注意は「適用するときの注意」節のとおり (H1 適用後の `grep -rn full-auto _claude/skills` の目視は未実施)
