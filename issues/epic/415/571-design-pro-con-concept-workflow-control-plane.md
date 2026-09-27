# 571 (design): pro-con のコンセプトを AI coding workflow / control plane として明文化する

起票日: 2026-09-27

> 番号 563 から改番 (2026-09-28。pending の 563-bug-nvim-plugin-checkout-drifts-from-lazy-lock と衝突していたため)。旧番号 563 で pro-con の control plane を指している話ならこの issue。

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

pro-con は当初「PM (producer) と PG (consumer) を分け、複数の Claude Code session を並列に回す TUI」として始まった。
しかし 2026-09-27 時点では、単なる multi-agent / worktree / TUI の session manager より広い責務を持っている。

競合調査でも、以下はすでに一般的な機能になっている。

- 複数 coding agent の並列起動
- task / workspace ごとの git worktree
- TUI / GUI から session を選んで見る
- agent の live log / transcript
- diff review
- follow-up instruction
- merge / PR
- agent 間の task list / dependency / message

したがって pro-con のコンセプトを「Claude をたくさん起動する TUI」と置くと、Claude Code 本体や既存 orchestrator と正面衝突し、何を自前で持つべきか判断しにくい。

**pro-con の中心を、coding agent の実行そのものではなく、人間の依頼を durable な仕事として受け取り、計画・実行・質問・レビュー・取り込み・終了まで追跡する local workflow / control plane として明文化する。**

この issue は実装を直ちに変えるものではない。まず README / epic 415 / 画面上の説明などで、今の実装から読み取れるコンセプトと非目標を揃える。

## 一文での定義

> pro-con は、coding agent を並列に起動するための TUI ではなく、人間の依頼を失わず、PM が仕事へ分解し、executor に配送し、質問・レビュー・取り込み・終了まで状態機械で追跡する local AI coding workflow / control plane である。

短く呼ぶ場合は **AI coding workflow engine** または **local control plane for coding agents** とする。

「agent orchestrator」だけでは、Claude Squad / conduct / Conductor 等との違いが伝わりにくいので主語にしない。

## 今の pro-con にすでにあるもの (2026-09-27)

正本は親 [415](415-design-claude-pm-worker-orchestration.md)、PM の規律は [pm-guide.md](../../../src/pro-con/pm-guide.md)。

### 1. 人間の依頼を card として durable にする

- 人間の依頼 1 件を card 1 枚として残す
- card は作成から終了まで必ずどこか 1 つの状態にいる
- 依頼の原文、履歴、担当、issue との対応を追跡する
- issue が無い終了カードも「回答済み / 調査のみ / 却下 / issue 化待ち」等の終わり方を持つ
- 「会話にはあったが task として行方不明」を不変条件として許さない

ここが session manager と最も違う。

### 2. PM / PG / integrator / dispatcher を分ける

- **PM**: 人間の依頼を受ける。card 化、issue 化、分解、依存関係の判断、PG からの質問への回答 / 人間への handoff を担当
- **PG**: 個別の実装・調査を行う worker。card 単位で worktree / session を持つ
- **integrator (取り込みの係)**: PG の成果を review し、必要なら rework、問題なければ master へ取り込んで card / issue を閉じる
- **dispatcher**: card の状態遷移、PG の起動・再開・停止、枠、依存待ち等を機械的に管理
- **monitor / watchdog**: 停滞、長時間待ち、異常を検知し、既知の待ちと「黙って止まった」を分ける

LLM の判断と、状態遷移・プロセス管理の責務を分離している。

### 3. card を中心に workflow を持つ

概念上の主な流れ:

```text
人間
  ↓
PM
  ↓
card
  ↓
計画 / issue / dependency
  ↓
PG の空き・利用枠・依存待ち
  ↓
PG
  ├─ 質問 → PM → 必要なら人間
  ├─ 追加オーダー / redirect
  └─ 完了
       ↓
     review
       ↓
   integrator
    ├─ rework → 同じ PG / session を再開
    └─ merge → card / issue を閉じる
```

状態名や UI 表示は実装に合わせるが、「executor session」ではなく **card が workflow の単位**であることをコンセプト上の正本にする。

### 4. 並列実行を安全側へ寄せる

- PG ごとの worktree
- 触る場所だけでなく「同じ判断・不変条件を変えるか」で衝突を見積もる
- `--after` で dependency を明示できる
- test / xcodebuild / device 等の占有 resource を直列化する設計
- PG の同時実行枠を dispatcher が管理
- worktree / branch / session の後始末を workflow の終了条件に含める

「何個起動できるか」より「並列化しても仕事を失わない・壊さない」を優先する。

### 5. 人間との境界を workflow に含める

- PG の質問は card の履歴に残す
- PM が答えられる実装上の質問は PM が処理する
- 目的・見た目・scope・破壊的操作・権限・お金等、人間が決めるべきものだけ handoff する
- 人間の回答後は同じ card / session を再開する
- 作業中の追加指示は `card order` として記録し、単なる session への野良メッセージにしない
- 方針変更は `--redirect` として途中の作業を止めて届ける

### 6. 観測・復旧を第一級に扱う

- `pro-con card list / show / wait`
- `pro-con log` のイベントログ
- running card の activity / command / progress
- `pro-con ps`
- viewer / screen relay
- dispatcher / TUI が落ちても file 上の正本から復元できる設計
- card と session / worktree の食い違いを黙って捨てない

UI は TUI だが、TUI 自体を正本にしない。

## 競合・近いツールの調査 (2026-09-27)

### Claude Code Agent Teams

公式:
- https://code.claude.com/docs/en/agent-teams

近いところ:

- team lead + teammates
- 各 teammate は独立した Claude Code instance / context
- shared task list
- task の pending / in progress / completed
- task dependency
- teammate 同士の message / mailbox
- lead からの assignment と teammate による claim
- teammate を直接選んで会話できる
- tmux / in-process の表示

**かなり近い。特に「PM + PG pool + task dependency + message」は Claude Code 本体に入り始めている。**

一方、公式 docs 自身が、Agent Teams は experimental で、session resumption / task coordination / shutdown に既知の制約があり、same-file edit や dependency の多い sequential work では single session / subagent が向くとしている。

pro-con が今持っている以下は Agent Teams の shared task list より上位の workflow として残る余地がある。

- 人間の依頼を必ず card 化する不変条件
- GitHub issue との対応と終了理由
- PM が問いそのものを整える工程
- human handoff
- rework
- integrator review
- merge / cleanup を含む終了条件
- resource serialization
- watchdog / event log / crash recovery

### Agent Deck / Conductor

- https://github.com/asheshgoplani/agent-deck
- https://github.com/asheshgoplani/agent-deck/blob/main/docs/conductor/README.md

近いところ:

- persistent な supervisor (Conductor)
- supervisor が複数 worker session を監督
- worker への instruction
- worker が waiting / error のとき判断
- supervisor が答えられないものを人間へ escalation
- `state.json` と `task-log.md` による durable state / log

pro-con の PM / monitor / human handoff とかなり近い。

違いとして、pro-con は **card の lifecycle と Git / review / issue の一貫した状態機械**を中心にしている。

### Conductor

- https://www.conductor.build/docs/concepts/workflow
- https://www.conductor.build/docs/concepts/parallel-agents
- https://www.conductor.build/docs/concepts/git-worktrees
- https://www.conductor.build/docs/guides/review-and-merge

近いところ:

- workspace ごとの branch + git worktree
- 複数 Claude Code / Codex / Cursor session の並列実行
- GitHub issue から workspace を作れる
- diff review
- agent review
- checks / CI / comments / todos
- PR / merge / archive

実行環境としては非常に近い。

ただし基本単位は人間が作る **workspace**。
pro-con は **人間 → PM → card → dependency / queue → executor → integrator** まで workflow 側が仕事を配送することを目標にしている。

### Vibe Kanban

- https://github.com/BloopAI/vibe-kanban

2026-09-27 時点で README に sunsetting の告知あり。

近いところ:

- Kanban task
- task attempt ごとの git worktree
- 複数 coding agent backend
- real-time logs
- agent への follow-up
- diff review
- merge
- task lifecycle の可視化

見た目・操作モデルでは pro-con にかなり近い競合だった。

ただし Vibe Kanban は project/task/workspace の GUI が主で、pro-con の PM が人間の依頼を受けて自律的に issue / dependency / human handoff / review workflow へ流す設計とは焦点が違う。

### Claude Squad

- https://github.com/smtg-ai/claude-squad

近いところ:

- TUI
- tmux
- 複数 coding agent session
- git worktree
- session の一覧・切替

これは **session manager / terminal orchestrator** としての pro-con に近い。

今の pro-con は card / PM / integrator / dispatcher を持つため、Claude Squad より上位の workflow を責務にしている。

### conduct

- https://github.com/ldlac/conduct

「small, local, open clone of Conductor as a TUI」を明示している。

近いところ:

- terminal orchestrator
- workspace ごとの worktree + branch
- Claude Code / Codex / OpenCode
- live output
- diff review
- merge
- agent への追加 instruction
- persisted workspace state

**TUI + worktree + multi-agent + diff + merge は、すでに独自性にはならない**ことを示す分かりやすい例。

### 追加で近いもの

調査中に以下も同じカテゴリで見つかった。

- Craig: https://github.com/hallsamuel90/craig
  - coding agent を worktree / branch / PTY 単位で持ち、changes / PR / checks を TUI にまとめる
- groundcrew: https://github.com/ClipboardHealth/groundcrew
  - backlog から local interactive coding agent へ dispatch、task ごとに worktree
- claude-architect: https://github.com/PyModel/claude-architect
  - architect が isolated implementer に委譲し、独立 verification 後に human approval されたものだけ merge する

この周辺は 2026 年にかなり混雑している。

## 競合比較から分かること

### すでに commodity に近いもの

以下を pro-con の主な売りとして扱わない。

- TUI で複数 agent を見る
- Claude / Codex 等を N 個起動する
- tmux
- git worktree
- session persistence
- live logs
- diff viewer
- agent に follow-up を送る
- merge button / command
- basic task list
- basic dependency graph

これらは必要な UX / implementation ではあるが、**コンセプトの中心ではない**。

### pro-con が中心に置くもの

1. **Human intent durability**
   - 人間の依頼を必ず card にする
   - 依頼が会話や session の中だけに消えない

2. **Work lifecycle**
   - request → plan → execute → question → review → rework / integrate → close
   - 仕事そのものを状態機械で持つ

3. **Decision routing**
   - PM が実装判断を吸収
   - 人間にしか決められない問いだけ human handoff
   - 何を誰が決めたか card history に残す

4. **Safe parallelism**
   - 数を増やすことより、dependency / conflicting invariant / resource lock / worktree isolation を扱う

5. **Independent review / integration**
   - worker の「できた」を完了としない
   - integrator が diff / test / context を見て rework または merge する

6. **Recoverability / observability**
   - UI / agent / dispatcher が落ちても仕事を復元できる
   - 黙って消えた card / worker を許さない

7. **Executor independence**
   - Claude Code を現在の executor として使うが、pro-con の card / workflow の意味を Claude Code 固有 session API に閉じない

## architecture の境界

長期的には次の分離を目標にする。

```text
┌─────────────────────────────────────────┐
│                pro-con                  │
│          workflow / control plane       │
│                                         │
│ human request                           │
│      ↓                                  │
│ PM → cards → dependency / queue         │
│      ↓                                  │
│ decision routing / human handoff        │
│      ↓                                  │
│ executor adapter                        │
│      ↓                                  │
│ review / rework / integrate / close     │
│                                         │
│ history / event log / recovery          │
└─────────────────────────────────────────┘
                    │
          executor interface
                    │
       ┌────────────┼─────────────┐
       ↓            ↓             ↓
 Claude Code     Agent Teams     Codex ...
 sessions
```

Claude Code Agent Teams が task / dependency / teammate lifecycle を十分安定して提供するなら、pro-con のその部分を二重実装せず executor adapter の内部へ委譲できる。

逆に、以下は Claude Code に委譲しない。

- card の正本
- human request の原文と lifecycle
- GitHub issue との対応
- human handoff の履歴
- review / rework / integration の workflow
- pro-con 全体としての event log / recovery invariant

**下位 executor が変わっても card の意味が変わらない**ことを境界の基準にする。

## 非目標

pro-con 自体が以下の競争をすることを目的にしない。

- Claude Code より優れた agent runtime を作る
- tmux replacement を作る
- Git worktree manager 単体として勝つ
- IDE / editor を作る
- 汎用 project management SaaS を作る
- LLM 自体の agent-to-agent protocol を独自標準化する

必要なら既存の executor / runtime の機能を使う。

## 使い分け

既存の親 issue の判断もコンセプトへ残す。

### pro-con を使わない方がよい

- ゼロから設計を固める途中
- 人間と Claude の短い往復で問い自体を変えていく
- 1〜数分で終わる小さな変更
- 同じファイル / 同じ判断を短い周期で何度も直す
- 敵対的レビューを短いサイクルで何周も回す

直接の Claude Code session の方が速い。

### pro-con が効く

- 問い / acceptance criteria が固まった仕事が複数ある
- 小さく比較的独立した修正が五月雨式に出る
- 2〜4 以上の作業を並行させる
- 「何を頼んだか」「どこまで終わったか」を人間が覚えたくない
- worker の成果を review / test / integration まで含めて流したい
- 人間は設計・優先順位・本当に人が決める問いへ集中したい

## 今後の設計判断のルール

新機能を pro-con に足す前に、次を順に問う。

1. それは **仕事の lifecycle / decision routing / safety / observability** に関する機能か
   - yes: pro-con の責務候補
2. それは単なる **agent runtime / worktree / session / terminal** の機能か
   - yes: Claude Code / executor / OS / 既存ライブラリへ委譲できないか先に調べる
3. Claude Code Agent Teams 等に同じ primitive があるか
   - ある: adapter で使えない理由を書いてから自前実装する
4. executor を Claude Code から Codex 等へ替えたときにも意味が残るか
   - 残る: control plane の機能
   - 消える: executor adapter 側の機能

## この issue でやること

- [ ] README / pro-con の概要を「multi-agent TUI」中心から workflow / control plane 中心の説明へ直す
- [ ] 親 415 の冒頭に一文のコンセプトと非目標を置く
- [ ] 現在の module / package を control plane と executor-specific な部分に棚卸しする
- [ ] Claude Code 固有の起動・resume・agents / teams 操作を executor adapter の境界として整理する
- [ ] Agent Teams に委譲できる既存機能と、pro-con に残すべき invariant を一覧にする
- [ ] 新しい feature issue では、上の「設計判断のルール」を使って責務を判断する

## 受け入れ条件

- [ ] 初見の人が README の冒頭だけで「Claude Squad / conduct と何が違うか」を説明できる
- [ ] TUI / worktree / multi-agent 自体を pro-con の中心的な独自性として説明していない
- [ ] card が workflow の正本であり、agent session は executor であると明記されている
- [ ] Claude Code Agent Teams と重複する領域 / pro-con に残す領域が文書化されている
- [ ] executor 固有機能を追加するとき「なぜ adapter への委譲では足りないか」を判断できる
- [ ] 既存の 415 / pm-guide の要件を落とさず、新しいコンセプトとの対応が読める
