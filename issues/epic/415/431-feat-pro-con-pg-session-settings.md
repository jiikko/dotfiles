# 431 (perf/design): pro-con の persistent role session 設定を固定し、Opus 5.5 の prompt-cache affinity を最大化する

> 🚨 **担当中: dotfiles-40**（2026-09-29〜）

起票日: 2026-09-24  
設計更新: 2026-09-29

親: [415](415-design-claude-pm-worker-orchestration.md)  
実測: [449](pending/449-research-pro-con-pg-startup-cost.md) / [525](525-perf-pro-con-pg-resume-cache-miss-tools-order.md) / [546](546-research-pro-con-endconversation-gate-resume-cache.md)

## 結論

この issue の目的を、当初の「PG 用の設定を軽くする」から次へ更新する。

> **PG / PM / 取り込みの係の session に渡す cache-shaping な入力を一箇所で固定し、card ごとに session を捨てても、Opus 5.5 の cross-session prompt cache を最大限再利用できる形にする。**

2026-09-29 時点の判断:

1. **「card 1 枚 = 新しい session」は維持する。PG pool は作らない**
   - 449 の実測では、新規 PG の固有コストは最初の応答まで 3〜8 秒・cache write 約 23.5k。
   - 新規 session でも system / tools 側の約 24.6〜26.6k は既に cache read できている。
   - session を使い回すと前カードの会話まで次カードで読み続け、context 汚染・stop / worktree / card の 1:1 対応も崩れる。
   - 「session を再利用する」のではなく **stable prefix を再利用する**。

2. **431 では cache の形を pro-con 側で揃える**
   - 起動と再開で model 以外の session-shaping flags / settings を揃える。
   - user の hook / permissions / rules を偶然取り込まない。
   - dynamic な値を settings / system prompt に足さない。
   - PG / PM / 取り込みの係が同じ `ExecLauncher` を通る事実をコードとテストに反映する。

3. **`--exclude-dynamic-system-prompt-sections` は PG には使わない**
   - 当初のこの issue では候補にしていたが、現在の Claude Code では **print / headless (`-p`) 向け**。
   - `claude --bg` の persistent session には効かない。431 の実装候補から外す。
   - upstream が interactive / background session に対応したら再評価する。
   - 参考:
     - https://code.claude.com/docs/en/cli-reference
     - https://github.com/anthropics/claude-code/issues/87282

4. **resume の大きな cache miss は 431 で無理に直さない**
   - 525 / 546 で、別 process になった resume が会話部分を再 cache する現象を追っている。
   - gate / `SendFeedback` / `EndConversation` を揃えるだけでは解消しなかった。
   - 431 は「pro-con が自分で揺らしている入力を無くす」までを担当し、その後も外れるなら 546 / upstream の問題として扱う。

5. **先に計測の料金を正す**
   - `src/pro-con/metrics/usage.go` は cache read を全 model 一律 0.1x としている。
   - 現在の Opus 5.5 は cache hit / refresh が base input の **0.05x** ($0.20 / MTok、base input $4 / MTok)。
   - 今のままだと pro-con 自身が「cache を効かせた価値」を約 2 倍高く見積もる。
   - 431 の before / after を判断する前に直す。

## なぜこの設計にするか

Anthropic の prompt cache は session ID ではなく、request の **prefix** を見る。

公式 docs:
- https://platform.claude.com/docs/en/build-with-claude/prompt-caching
- https://platform.claude.com/docs/en/about-claude/models/optimizing-for-cost-and-intelligence

cache の対象順は概念上:

```text
tools
  ↓
system
  ↓
messages
```

同じ workspace 内で同じ prefix を送れば、別 session / 別 request でも cache hit できる。
したがって、

```text
C-101 → session A → 終了
C-102 → session B → 終了
```

でも、先頭の tools / system / 共通 context が同じなら cache は共有できる。

逆に、前の方にある tools / system prompt / model / thinking mode 等が変わると、その位置以降の cache を失う。

Opus 5.5 の現在の単価:

| 種類 | $ / MTok | base input 比 |
|---|---:|---:|
| input | 4.00 | 1.00x |
| 5m cache write | 5.00 | 1.25x |
| 1h cache write | 8.00 | 2.00x |
| cache read / refresh | 0.20 | **0.05x** |
| output | 20.00 | — |

つまり Opus 5.5 では **既にある cache を読むこと自体が非常に安い**。
card をまたいで会話を引き継ぐより、各 card を隔離したまま共通 prefix の hit 率を上げる方を優先する。

## 現在の実装 (2026-09-29)

### persistent role の起動口

`src/pro-con/dispatcher/launcher.go` の `ExecLauncher` を以下が共用している。

- PG
- PM
- 取り込みの係 (integrator)

PG だけの launcher ではない。

起動:

```text
claude --bg
  -w <name>
  -n <name>
  --setting-sources project,local
  --settings <sessionSettings>
  <prompt>
```

再開:

```text
claude --bg
  --resume <session-id>
  -n <name>
  --setting-sources project,local
  --settings <sessionSettings>
  <resume text>
```

`sessionSettings` は現在:

```json
{
  "autoMemoryEnabled": false,
  "feedbackDrafts": "off",
  "language": "<user settings の language があれば>"
}
```

### 今、入らないもの

`--setting-sources project,local` により user settings 由来の以下は persistent role へ持ち込まない。

- user hooks
- user permissions
- `~/.claude/rules/`
- user settings の model 等

`~/.claude/CLAUDE.md` は home が cwd の祖先なので Project 扱いで残る。
2026-09-26 の `/context` では、PG の Memory files は約 14.1k (home の CLAUDE.md 約 8.5k + project rules)。
auto memory は外している。

### model

PM / PG / integrator は `--model` を明示していない。
[514](done/514-feat-pro-con-codex-adversarial-review-option.md) の 2026-09-26 実測では、取り込みの係 616 応答・PG 1549 応答はすべて `claude-opus-5-5`。

431 では model を pin しない。
model の固定は品質・利用枠・fallback の意味も変えるため、「cache のためだけ」に導入しない。
将来 role ごとの model 設定を入れる場合は、model が cache identity の一部であることを前提に設計する。

### haiku の使い捨て role

要約 / btw は `dispatcher/runner.go` で:

```text
claude -p
  --model haiku
  --no-session-persistence
  --setting-sources project,local
  --settings <HaikuSettings>
```

を使う。

persistent role と寿命も用途も違うので 431 の cache affinity の対象から分ける。
`--exclude-dynamic-system-prompt-sections` を検証するならこちらの `-p` 系が対象になり得るが、PG の改善とは別件。

## 既存の実測を更新後の前提として残す

### 449: card ごとに新規 session を起こすコスト

Claude Code 2.1.282 / Opus 5.5 / C-002〜C-006:

- 新規 PG の最初の context: 約 48〜50k
- そのうち cache write: 約 23.5k
- cache read: 約 24.6〜26.6k
- 最初の応答まで: 3〜8 秒
- 新規 session でも先頭の共通部分は既に cross-session で読めている
- card 全体では、起動 write は total input の 0.3〜2.9% 程度だった
- そのため **PG pool は採らない**と決めた

この判断は維持する。

### 525 / 546: resume の外れ

449 の後、resume 87 回中 22 回で大きな cache miss を観測。
計 3.20M tokens を再 write し、当時の PG cache write 全体の約 36% を占めた。

525 で `feedbackDrafts: "off"` を入れて `SendFeedback` の揺れを止めたが、外れは残った。

546 では:

- `DISABLE_GROWTHBOOK` で gate を固定
- `EndConversation` / `SendFeedback` を揃える
- system prompt / tools を比較

まで行ったが、別 process での resume は依然として会話部分を書き直した。

したがって現在の優先順位は:

```text
A. pro-con 自身の session profile の揺れを無くす    ← 431
B. その状態で計測する
C. まだ resume が外れるなら Claude Code 側を追う    ← 546
```

## 2026-09-29 に削除する古い前提

この issue の古い記述には以下が混ざっていたので、今後の判断には使わない。

### 「起動時に約 13 万 token」

2026-09-24 の初期状態では user rules 等を大量に読み、1 語を返すだけでも約 12〜13 万 token だった。
現在は `--setting-sources project,local` / role settings により大幅に減っている。

現在の比較基準は 449 の **最初の要求 約 48〜50k** とする。

### 「--setting-sources では ~/.claude/rules が外れない」

Claude Code 2.1.281 時点の観測と 2.1.282 の観測が食い違っていた。
現在の pro-con / 2.1.282 では `--setting-sources project,local` の PG に user rules は載っていないことを実測済み。

今の動作を正とする。

### 「--exclude-dynamic-system-prompt-sections を PG に足す」

採らない。
現在の Claude Code では `-p` / headless 用で、`--bg` の persistent session には適用されない。

## 設計上の不変条件

431 の実装では次を守る。

### 1. card isolation を壊さない

- 1 card = 1 PG session = 1 worktree
- 別 card の transcript を引き継がない
- pool 化しない

### 2. persistent role の cache-shaping profile は一箇所で作る

起動と再開で次が別々に組み立てられないようにする。

- `--setting-sources`
- `--settings`
- 将来追加される model / effort / tool surface に影響する flag

start と resume の差は lifecycle 上必要なものだけ:

- start: `-w <name>`
- resume: `--resume <id>`
- positional text

`-n`、setting sources、role settings 等は共通 profile から出す。

### 3. volatile な値を --settings に入れない

次を role settings に足さない。

- card ID
- issue number
- cwd / worktree path
- git status
- timestamp
- session ID

card 固有情報は user message / card prompt 側へ置く。

### 4. user settings を丸ごとコピーしない

現在どおり user settings からは必要な値だけ選ぶ。

今は:

- `language`

だけ。

以下はコピーしない。

- hooks
- permissions
- model
- effort
- MCP
- statusLine
- user rules を戻す設定

理由: role の tool / prompt shape を人間の通常 session の設定変更に引きずらせない。

### 5. ~/.claude/rules は戻さない

旧 431 の未決事項だった「PG に user rules の一部を戻すか」は **431 では戻さない**で閉じる。

必要な規律は次のどちらかへ置く。

- repo 自身の `CLAUDE.md` / `.claude/rules/`
- pro-con が role に渡す guide / prompt

「user rules を部分 allowlist で戻す」は:

- cache prefix を増やす
- user 側の変更で PG の context が変わる
- skill / agent / permissions まで user source から戻る可能性がある

ため、明確な欠落が観測されたときに別 issue で扱う。

### 6. custom system prompt は使わない

cache を自前で完全制御するために `--system-prompt` で Claude Code の default prompt を置き換える案は採らない。

Claude Code の built-in agent behavior / tool instructions を失う方がリスクが大きい。

## 実装設計

### Step 1: metrics の Opus 5.5 cache read 単価を修正する

対象:

- `src/pro-con/metrics/usage.go`
- 対応する test

今:

```text
cache read = input * 0.1
```

を全 model に適用している。

変更案:

```go
// model ごとに cache read multiplier を持つ。
// Opus 5.5 は公式料金どおり 0.05、その他の現行 model は 0.1。
type price struct {
    in, out       float64
    cacheReadRate float64
}
```

書き込みは既存どおり:

- 5m = 1.25x
- 1h = 2x

を使う。

#### テスト

- Opus 5.5 の cache read 1M = $0.20
- Sonnet 5 の cache read 1M = $0.20 (base $2 × 0.1)
- Haiku 4.5 の cache read 1M = $0.10
- write / input / output の既存計算を壊さない

🚨 `FiveHourUSDPerPct = 3.5` は 449 の粗い換算なので、この task では再推定しない。
表示上「粗い値」である説明を維持する。

### Step 2: persistent session profile を共通化する

対象:

- `src/pro-con/dispatcher/launcher.go`
- `src/pro-con/dispatcher/rolesettings.go`
- `src/pro-con/dispatcher/launcher_test.go`

名前は実装時に変更してよいが、概念として:

```go
persistentRoleSettings(userSettings)
persistentSessionArgs(name)
```

のように分ける。

形:

```text
start
  --bg -w <name>
  + persistentSessionArgs(name)
  + prompt

resume
  --bg --resume <session-id>
  + persistentSessionArgs(name)
  + resume text
```

`persistentSessionArgs` が持つもの:

```text
-n <name>
--setting-sources project,local
--settings <persistentRoleSettings>
```

これにより「start には付けたが resume には忘れた」「PM だけ別 settings」等で cache shape がずれる変更を起こしにくくする。

### Step 3: settings の意味をコード上で正す

今の `sessionSettings` のコメントは「PG・PM」と書いているが、`role.go` の integrator も同じ `d.Launch.Start / Resume` を通る。

コメント / test の用語を:

> PG / PM / integrator の persistent role session

へ直す。

中身は当面:

```json
{
  "autoMemoryEnabled": false,
  "feedbackDrafts": "off",
  "language": "..."
}
```

のまま。

### Step 4: cache-shaping invariant のテストを足す

既存の exact args test に加え、意図を直接固定する。

最低限:

1. start / resume の persistent profile が同じ
2. user settings の以下を渡さない
   - `model`
   - `effortLevel`
   - `hooks`
   - `permissions`
3. language だけは渡す
4. auto memory / SendFeedback は起動・再開とも同じ値
5. `--exclude-dynamic-system-prompt-sections` を `--bg` に付けない
6. card ID / cwd 等を `--settings` に混ぜない

exact な引数列の test だけでなく、「cache profile が start / resume で同一」という性質を 1 本の test で表す。

### Step 5: 実装後に本物で再計測する

新しい synthetic PG を大量に起こさず、通常の dogfooding の transcript を使う。

対象:

- 取り込み後の新規 PG start: 最低 10 件
- 同じ期間の resume: 最低 10 件
- model は集計時に分ける。Opus 5.5 だけの群を 449 と比較する

見る値:

```text
新規 card:
  first request cache_read_input_tokens
  first request cache_creation_input_tokens
  first request total context

resume:
  first request cache_read_input_tokens
  first request cache_creation_input_tokens
  prefix_mismatch / cache miss reason (取れる版なら)
```

Claude Code 2.1.260 以降は prompt-cache の hit ratio / miss / likely cause を `/usage`・status line 側にも持つ。
transcript に diagnostics が記録される版ではそれも材料にし、無ければ従来どおり usage tokens から判定する。

参考:
- https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md
- 2.1.260: prompt-cache hit ratio / misses / likely cause

### 比較基準

449 の baseline:

```text
新規 PG 最初の要求:
  context      48〜50k
  cache write  約23.5k
  cache read   約24.6〜26.6k
```

431 の変更自体は semantics を変えず profile を固定するものなので、「必ず write が X%減る」を受け入れ条件にはしない。

見るのは:

- 新規 card の shared prefix read が消えていない
- first request の write が理由なく大幅に増えていない
- start / resume の pro-con 由来の profile 差が無い
- resume miss が残るなら 546 に数字を書き戻せる

## 今回はやらないもの

### PG pool

449 の再評価 trigger までは作らない。

trigger:

- 1〜2 turn で終わる小カードが多数を占め、起動 write 約 23.5k が card cost の主要部分になった
- cross-session prefix がほとんど hit しなくなった

### 1 時間 TTL の強制

Claude Platform には 1h cache があるが、Claude Code / subscription / provider ごとの扱いを 431 から直接制御しない。

将来 `promptCacheTtl` 等を pro-con が設定するなら:

- subscription で効くか
- 現在の session が実際に 5m / 1h のどちらを使っているか
- card / resume の間隔分布

を測って別 issue で決める。

### model pin

431 では `--model opus` 等を足さない。

role ごとの model を設定するなら [426](done/426-design-pro-con-open-decisions.md) の構想どおり config の責務として設計し、cache だけを理由に黙って品質設定を変えない。

### DISABLE_GROWTHBOOK

546 で cache miss に効かなかった。
全 gate を既定値へ倒す副作用だけ残るので入れない。

### --exclude-dynamic-system-prompt-sections

persistent `--bg` には使わない。
Claude Code upstream が background / interactive session に対応したら、同じ 449 の測り方で A-B してから採用する。

## 変更するファイル

実装の第一候補:

- `src/pro-con/metrics/usage.go`
- `src/pro-con/metrics/*_test.go`
- `src/pro-con/dispatcher/launcher.go`
- `src/pro-con/dispatcher/rolesettings.go`
- `src/pro-con/dispatcher/launcher_test.go`
- この issue (実測結果を書き戻す)

必要にならない限り触らない:

- card / store の schema
- dispatcher の card state machine
- worktree lifecycle
- config.toml
- UI

## 実装順

1. [ ] Opus 5.5 の cache read multiplier を 0.05 に直し、料金 test を追加
2. [ ] `sessionSettings` / launcher の責務名を persistent role (PG / PM / integrator) に合わせる
3. [ ] start / resume の共通 cache-shaping profile を 1 関数に寄せる
4. [ ] cache profile invariant の test を追加
5. [ ] `~/.claude/rules` を戻さないことをコードコメント / test で確定
6. [ ] `make test` / `make lint`
7. [ ] 取り込み後の dogfooding 10 start / 10 resume 以上を集計
8. [ ] 449 baseline と比較し、この issue に結果を書く
9. [ ] resume miss が残るなら 546 に現在版の実測を追記し、431 は閉じる

## 受け入れ条件

- [ ] Opus 5.5 の cache read を $0.20 / MTok (0.05x) で計算する
- [ ] PG / PM / integrator の persistent session settings の正本が 1 箇所にある
- [ ] start / resume が同じ cache-shaping profile を使うことを test が保証する
- [ ] user settings の model / effort / hooks / permissions を persistent role に持ち込まない
- [ ] user rules を 431 では戻さない
- [ ] `--exclude-dynamic-system-prompt-sections` を PG の解決策として扱っていない
- [ ] card 1 枚 = session 1 本の不変条件を維持する
- [ ] 変更後の本物の Opus 5.5 で新規 start / resume の cache read / write を再計測している
- [ ] 新規 card の cross-session cache hit が維持されている
- [ ] resume の外れが残る場合、その責務が 546 に切り分けられている
- [ ] `make test` / `make lint` が通る

## 過去の経緯 (圧縮)

### 2026-09-24

初期 PG は user hook / rules を大量に読み、単純な起動でも約 12〜13 万 token。
Stop hook が無関係な issue を触ろうとする問題もあった。

### 2026-09-26

`--setting-sources project,local` と `--settings` を使い:

- user hooks を外す
- auto memory を外す
- language だけ user settings から渡す
- PG の context を大きく削減

まで実装。

PG に user rules の一部を戻す案は保留していたが、2026-09-29 のこの更新で **戻さない**と決めた。

### 2026-09-26〜27

449 / 525 / 546 で cache を実測。

- card ごとの新規 session 自体は主犯ではない
- shared prefix は新規 session 間でも読める
- 大きい損は一部の resume の再 write
- tools / gate を揃えても resume miss は完全には消えない

この結果を受け、431 の責務を「PG を軽くする」から **persistent role の cache-shaping profile を安定させる**へ更新した。
