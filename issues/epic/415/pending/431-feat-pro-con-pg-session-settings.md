# 431 (perf/design): pro-con の persistent role session 設定を固定し、Opus 5.5 の prompt-cache affinity を最大化する

起票日: 2026-09-24  
設計更新: 2026-09-29

> **保留 (2026-09-29)**: 実装 (実装順 1〜6) は済み。残りは今の形での計測 (実装順 7〜9) だけ。
> **再開の trigger**: dogfooding を再開して、今の形で起動された PG の起動が 10 本・再開が 10 回たまったとき。**dogfooding を再開する前に
> `pro-con config set schedule off`** (完了したカードの transcript を毎日 04:00 の clean が消すため。数え終えたら on に戻す。「進捗」の「残タスク」)。
> 今の形の 7 本で見えた外れは PM・取り込みの係の TTL 切れで、431 の範囲外 ([581](../done/581-perf-pro-con-role-resume-after-ttl-rewrites-whole-conversation.md))。
> 581 で解消 (2026-10-01): 前の応答から 55 分以上空いた役は再開せずに同じ worktree で起動し直す。計測で PM・取り込みの係の TTL 切れの外れを数えるときは、起動し直しの書き込み (約 2.2 万の見込み) に置き換わっているはず。

親: [415](../415-design-claude-pm-worker-orchestration.md)  
実測: [449](../done/449-research-pro-con-pg-startup-cost.md) / [525](../525-perf-pro-con-pg-resume-cache-miss-tools-order.md) / [546](../546-research-pro-con-endconversation-gate-resume-cache.md)

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
   - (2026-09-29) 会話部分の書き直し (`messages_changed`) は実験だけで出た。本番の 60 分以内の外れは SendFeedback の出入りによる `tools_changed` だけで、
     今の形では起きにくい見込み (546 の「本番の再開の外れの内訳」)
   - 431 は「pro-con が自分で揺らしている入力を無くす」までを担当し、その後も外れるなら 546 / upstream の問題として扱う。

5. **先に計測の料金を正す** (2026-09-29 に直した。「進捗」)
   - 実装前の `src/pro-con/metrics/usage.go` は cache read を全 model 一律 0.1x としていた。
   - 現在の Opus 5.5 は cache hit / refresh が base input の **0.05x** ($0.20 / MTok、base input $4 / MTok)。
   - そのままだと pro-con 自身が「cache を効かせた価値」を約 2 倍高く見積もる。
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

## 実装前の形 (2026-09-29 朝。実装後の名前は `persistentSessionArgs` / `persistentSessionSettings` で、渡す引数は同じ)

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

`sessionSettings` の中身 (実装前も後も同じ):

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
[514](../done/514-feat-pro-con-codex-adversarial-review-option.md) の 2026-09-26 実測では、取り込みの係 616 応答・PG 1549 応答はすべて `claude-opus-5-5`。

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
(2026-09-29: これは実験の条件でだけ出た `messages_changed`。本番の外れの内訳は 546 の「本番の再開の外れの内訳」と、この issue の「計測」)

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

実装前:

```text
cache read = input * 0.1
```

を全 model に適用していた。

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

実装前の `sessionSettings` のコメントは「PG・PM」と書いていたが、`role.go` の integrator も同じ `d.Launch.Start / Resume` を通る。

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

role ごとの model を設定するなら [426](../done/426-design-pro-con-open-decisions.md) の構想どおり config の責務として設計し、cache だけを理由に黙って品質設定を変えない。

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

1. [x] Opus 5.5 の cache read multiplier を 0.05 に直し、料金 test を追加
2. [x] `sessionSettings` / launcher の責務名を persistent role (PG / PM / integrator) に合わせる (`persistentSessionSettings`)
3. [x] start / resume の共通 cache-shaping profile を 1 関数に寄せる (`persistentSessionArgs`)
4. [x] cache profile invariant の test を追加 (`TestPersistentSessionProfile`)
5. [x] `~/.claude/rules` を戻さないことをコードコメント / test で確定
6. [x] `make test` / `make lint`
7. [ ] 取り込み後の dogfooding 10 start / 10 resume 以上を集計 — 2026-09-29 時点で今の形の PG は起動 1・再開 2 (役を含めて 7 本。「計測」)。残タスクへ
8. [ ] 449 baseline と比較し、この issue に結果を書く — 7 本の分は比較した (「計測」)
9. [ ] resume miss が残るなら 546 に現在版の実測を追記し、431 は閉じる — 本番の外れの内訳を 546 に、PM・取り込みの係の TTL 切れを 581 に書いた。431 は計測待ちで閉じない

## 受け入れ条件

- [x] Opus 5.5 の cache read を $0.20 / MTok (0.05x) で計算する
- [x] PG / PM / integrator の persistent session settings の正本が 1 箇所にある (`persistentSessionArgs` / `persistentSessionSettings`)
- [x] start / resume が同じ cache-shaping profile を使うことを test が保証する
- [x] user settings の model / effort / hooks / permissions を persistent role に持ち込まない
- [x] user rules を 431 では戻さない
- [x] `--exclude-dynamic-system-prompt-sections` を PG の解決策として扱っていない
- [x] card 1 枚 = session 1 本の不変条件を維持する (起動・再開の判断は変えていない。dispatcher.go は Launcher の説明のコメントだけ)
- [ ] 変更後の本物の Opus 5.5 で新規 start / resume の cache read / write を再計測している — 今の形の 7 本は数えた。10 / 10 に届かない
- [ ] 新規 card の cross-session cache hit が維持されている — 未観測 (今の形の起動は冷えた 1 本だけ)。tools と system の先頭は温かい起動と同一
- [ ] resume の外れが残る場合、その責務が 546 に切り分けられている — 今の外れ: tools_changed は 546、TTL 切れは 581。10 / 10 を数えたら閉じる
- [x] `make test` / `make lint` が通る

## 進捗

### 2026-09-29 実装 (dotfiles-40)

commit「pro-con: Opus 5.5 のキャッシュの読みを料金 0.05 倍で数え、PG・PM・取り込みの係の起動と再開の session の形を 1 箇所に寄せる (431)」。
実装順 1〜6 と、受け入れ条件のうち計測以外。

- **単価** (claude-api skill 2.1.284 の表): Opus 5.5 は入力 $4 / 出力 $20 / 読み $0.20 (入力の 0.05 倍)、Sonnet 5・5.5 は $2 / $10 / $0.20、
  Haiku 4.5 は $1 / $5 / $0.10。書き込みはどの model も 5 分 1.25 倍・1 時間 2 倍。449 は Opus 5.5 の読みを 0.1 倍と置いていた
  (Sonnet / Haiku の読みは書いていない。当時の同梱の表は未確認)。
  `metrics/usage.go` の `price` は読みを入力に対する倍率 (`readRate`) で持つ
- **設計からの差分: 5 時間枠の %**。`FiveHourUSDPerPct = 3.5` は、449 が読みを 0.1 倍と置いた料金換算 ($120 / 34%) に合わせた値。
  読みの料金だけ 0.05 倍に直して同じ 3.5 で割ると、同じ使い方の % が 3 割ほど小さく出る (449 の 5 時間枠の区間は読みが料金換算の 63%)。
  この issue の「3.5 は再推定しない」を守るため、**% は読みを 0.1 倍の重み (`fiveHourReadRate`) で数えたまま、USD だけ料金どおりにした**。
  旧版 (9554235e) と新版に乱数の transcript 2,000 通りを読ませ、% の差 0 件 (USD は opus を含む 1,181 件で変わる) を確かめた
- **記録済みの USD**: `metrics.jsonl` のうち、この変更より前に閉じたカードの USD は読み 0.1 倍で数えた値のまま (Opus 5.5 の読みの分だけ高い)。
  % は前後で同じ式なので比べられる。`pro-con stats` の USD を前後にまたいで比べるときだけ注意
- **launcher**: 起動 `--bg -w <name>` / 再開 `--bg --resume <id>` の後ろに共通の `persistentSessionArgs(name, userSettings)`
  (`-n <name> --setting-sources project,local --settings <persistentSessionSettings>`) と位置引数。**渡す引数は 1 バイトも変えていない**
  (厳密な引数のテスト `TestLauncherArgsPassLanguageAndNoAutoMemory` を変えずに通る)
- **直したコメント**: `rolesettings.go` の「SendFeedback を外すのは PG・PM だけ (再開があるのはこの 2 つ)」は誤りだった
  (取り込みの係も `role.go` の `prepareRole` → `d.Launch.Resume` で再開し、同じ settings を受けていた)。launcher.go 冒頭の
  「まだ一度も本物の claude で走らせていない」も古い (514 の実測で PG 1549 応答)。`dispatcher.go` の `Launcher` の説明も PG・PM・取り込みの係に
- **検証**: `make -C src/pro-con test` (race) rc=0・`lint` 0 issues。変異 9 本で想定の検査が red:
  読みを 0.1 倍に戻す / % を料金の読みで数える / Sonnet の読みを半分に / 再開だけ自前の引数列 / ユーザーの effortLevel だけ写す /
  --settings に session の名前 / --exclude-dynamic-system-prompt-sections を足す / --setting-sources に user / 起動だけ --model。
  effortLevel だけ写す変異は、既存の厳密テストでは緑のまま新テストだけが落ちる (新テストにしか無い検出力)

### 2026-09-29 計測 (dotfiles-40。既存の transcript の読み取りだけ。LLM を呼んでいない)

**今の形で起動された役の session は 7 本 (PG は起動 1・再開 2) しかなく、「10 起動 / 10 再開」は満たせない**。今の形 = `feedbackDrafts: "off"` が入った
8ac575fb 以降。この issue の変更は渡す引数を変えていないので、今の形の計測がそのまま「変更後」の計測になる。

- 群の決め方: `~/.claude/jobs/<id>/state.json` の `respawnFlags` (claude が session を起こし直すときの flag。`--settings` を含む) に `feedbackDrafts` が在るもの。
  dispatcher が 8ac575fb を含む build (500537e) に切り替わったのは 09-27 08:22:40 JST で、7 本はすべてその後、前の形はすべてその前 (events と一致)
- pro-con は 09-27 18:41 から動いておらず、2.1.284 で起動された役の session は 0 本

| 役 | 09-27 JST | read | write | 前の応答からの間隔 | 結果 |
|---|---|---:|---:|---:|---|
| 取り込みの係 再開 | 08:23 | 713,690 | 1,322 | 5.1 分 | 当たり |
| PM 再開 | 10:54 | 0 | 459,646 | 174.7 分 | 外れ (TTL 切れ) |
| PG 起動 (C-102) | 10:54 | 0 | 48,423 | — | 冷えた起動 |
| PM 再開 | 10:57 | 460,786 | 1,053 | 2.7 分 | 当たり |
| PG 再開 (C-102) | 11:19 | 94,703 | 1,085 | 0.5 分 | 当たり |
| PG 再開 (C-102) | 11:26 | 133,350 | 972 | 0.9 分 | 当たり |
| 取り込みの係 再開 | 11:26 | 24,645 | 697,695 | 179.8 分 | 外れ (TTL 切れ) |

(claude 2.1.283、すべて claude-opus-5-5。比べた前の形: PG の起動 26 本・再開 69 回、PM の再開 92 回、取り込みの係の再開 58 回)

1. **新規 card の cross-session hit は、今の形ではまだ直接見えていない**。今の形の起動は C-102 の 1 本だけで、同じ tools の session が
   2 時間 31 分動いていなかった (1 時間 TTL の外) ため冷えていた。ただし C-102 の tools (15 本の定義の中身) と system の先頭ブロックは、前の形で
   SendFeedback 無しに起動した C-057 / C-087 / C-089 と 525 の probe の起動 1 本 (4b5dad9e。9132782b はその写し) と同一 (ハッシュ一致。
   温かい起動ではどれも 24,803 を読んだ)。温かければ同じ先頭を読むはず (推定)
2. **SendFeedback の揺れは今の形で止まっている見込みが高い**。前の形の PG の起動 26 本は、read が SendFeedback の有無で 2 つの値に分かれた
   (26/26。2.1.283 では 24,803 / 26,759)。flag 付きで gate の効く起動 3 本 (C-102 と 546 の実験 2 本。DISABLE_GROWTHBOOK 付きの 1 本を除く) はどれも
   「SendFeedback 無し・EndConversation の段あり」で、flag 無しの起動ではこの組が 0/278 (段があれば SendFeedback も在る: 107/107)。
   gate が true の起動でも `"off"` が SendFeedback を外している (間接の観測)
3. **再開の外れの理由は 2 つだけ** (transcript の `message.diagnostics.cache_miss_reason`。外れ 37 回すべてに付き、当たり・途中の要求・起動には付かない):
   `tools_changed` 15 回 (PG の、起動時に SendFeedback が在った chain だけ) と `previous_message_not_found` 22 回 (前の応答から 60 分越え = 1 時間 TTL 切れ)。
   60 分以内で外れた再開は全部 `tools_changed`。今の形の PG の再開は 2/2 当たった。**再開の側でも `"off"` は効いている見込み (間接の観測)**:
   今の形の再開したプロセス 6 本 (PG 2・PM 2・取り込みの係 2) は、途中で EndConversation を足し直した (その gate が true に戻った) ときに SendFeedback を
   足さなかった (6/6)。前の形の同じ場面では 77/81 で SendFeedback も足した。EndConversation と SendFeedback は別の gate
   (`tengu_umber_kestrel` / `tengu_juniper_relay`) なので、EndConversation の出入りは SendFeedback の gate を直接は表さない。内訳は 546 の「本番の再開の外れの内訳」
4. **今の形の 7 本で見えた外れは PM・取り込みの係の TTL 切れだけ** (2 回で 1.16M トークンの書き直し。PG の TTL 切れはまだ観測していない)。
   前の形を含めると PM・取り込みの係の TTL 切れは 1.5 日で 18 回・5.26M。PG の `tools_changed` は transcript が残った再開 (71/240 回) だけで 2.26M の下限
   (449 は C-002〜C-037 だけで 3.20M) なので、どちらが大きいかは言えない → [581](../done/581-perf-pro-con-role-resume-after-ttl-rewrites-whole-conversation.md)
5. 前の形の起動の書き込みが 449 (約 23.5k) より小さい 21.7〜22.6k なのは、`autoMemoryEnabled: false` を入れた後の起動 (18 本) の分。
   auto memory の分だけ文脈も同じく約 1.9k 小さい (読みの先頭は変わらない)

較正と反証:

- 較正: 449 の C-004 の起動・再開、525 の実測 3、546 の A-B を同じ数え方で数え直して全部一致。449 の C-002 / C-003 / C-005 / C-006 は transcript が消えていて数え直せない
- 反証 (別の数え方: requestId で束ね、再開の写しを uuid の包含で除く。cards.json と起動・再開の種別が 98/98 一致): 数字は全部再現した。解釈の補正で採ったもの:
  - 「2.1.283 では PM と PG で先頭を共有しない」→ 誤り。共有を決めるのは役ではなく、chain を起動した版の tools (同じ tools なら役をまたいで共有する。C-004 の PG と PM で一致)
  - 「read の値が 1 種類になったかは C-102 の 1 本では判定できない」→ tools のハッシュで判定できる (上の 1)
  - 再開 2/2 の読み方: 2/2 と「偶然なら約 9%」が示すのは起動の側の `"off"` の効き目 (起動の snapshot で直接見えている) で、
    再開の側の効き目は別の観測 (上の 3 の 6/6) で見えた (2026-09-29 の 2 周の敵対的レビュー)
- 🚨 **完了した PG カードの transcript と jobs は、dispatcher の予定 `worktree-clean` の `pro-con worktree clean --yes` が消す** (毎日 04:00。
  dispatcher を止めていたら、起動し直した直後の tick で 1 回追いつく。予定は `src/pro-con/schedule/schedule.go`、消す判定は `src/pro-con/wtclean/sessions.go` で、
  worktree とブランチを片付けられたカードの分だけ)。予定が入った 09-27 13:39 の初回で、PG の起動 90 本中 63 本・再開 240 回中 169 回の transcript が消えた。
  `metrics.jsonl` はカードごとの合計しか持たず、最初の要求の値は取れない。PM・取り込みの係の transcript は消されない。
  今の形の PG の 3 本 (C-102) も、次に dispatcher を起動した最初の tick の追いつきの clean で消える見込み (worktree に残るのは使い捨ての tmp/ だけ)。
  上の表の数字は残るが、ハッシュの突き合わせなどはその後できない

### 残タスク (計測待ち)

- [ ] 今の形 (Opus 5.5) の PG の起動 10 本・再開 10 回を数える (実装順 7・8)。**再開の条件**: dogfooding を再開して今の形の起動・再開がたまったとき。
  **dogfooding を再開する前に `pro-con config set schedule off`** (上の 🚨。dispatcher が止まっていても、次の起動の最初の tick で予定より先に効く。
  数え終えたら on に戻す)。起動ごとに、起動時刻の前 60 分に同じ tools の session が動いていたか (冷えた起動を分ける) と tools のハッシュを、
  再開ごとに `diagnostics.cache_miss_reason` を添える
- [ ] 今の形の再開したプロセスで、SendFeedback が一度も出てこない (起動の tools にも `deferred_tools_delta` にも `tools_changed` の外れにも) ことを見る。
  SendFeedback の gate の値そのものが要るなら、再開の時刻の `~/.claude.json` の `cachedGrowthBookFeatures.tengu_juniper_relay` を控える
- 次に数える session は 2.1.284 以降で動く。温かい起動の read の値と tools のハッシュは版ごとに取り直す (2.1.282 → 2.1.283 で read が +158)
- [ ] 数え終わったら受け入れ条件の残り 3 つを閉じる。`tools_changed` が残れば 546 へ、TTL 切れは 581 へ
- 数え方 (2026-09-29 の計測と反証で使った判定。スクリプトは残していないので、この手順から組み直す。transcript・jobs・cards.json / cards-archive.jsonl・events の読み取りだけで足りる):
  1. 対象: `~/.claude/jobs/<短い id>/state.json` の `respawnFlags` に `feedbackDrafts` が在る job が今の形。役は worktree 名 (pc-c-* / pc-pm-* / pc-int-*)
  2. 起動か再開か: transcript が前の session の応答 (同じ message.id / uuid) を写して持っていれば再開。時刻は cards.json と cards-archive.jsonl の履歴 (PG)、
     events.jsonl / events.1.jsonl の launch (PM・取り込みの係と、記録から消えたカードの PG) と突き合わせる (完了したカードは 24 時間で書庫へ移り、1 週間で書庫からも消える)
  3. 最初の要求: 起動・再開の時刻より後で、前の transcript に無い message.id の最初の応答の `message.usage` (同じ message.id は後の行を正、`<synthetic>` は除く)
  4. 外れ: 応答の `message.diagnostics.cache_miss_reason` (再開の外れにだけ付く。起動の応答には付かない)。間隔 = 再開の時刻 − 前の会話の最後の本物の応答。
     60 分越えは TTL 切れとして分ける
  5. 冷えた起動: 起動の前 60 分に、同じ tools (起動の transcript の `prompt_snapshot` の tools のハッシュ) の session の要求が無い
  6. SendFeedback の出入り: 再開したプロセスの `deferred_tools_delta` に SendFeedback が現れるか (EndConversation の出入りは別の gate で、SendFeedback の gate を表さない)
- 計測を楽にする案 (未決): カードを閉じるときに、起動・再開ごとの最初の要求の usage と `cache_miss_reason` を metrics.jsonl に残す (clean で消えない)

### 2026-09-29 敵対的レビュー (観点を分けた 3 体。codex は使わない)

全数: コード 6 件・issue の記述 13 件・完了条件と文書 13 件 (重なりを含む)。採った修正 (commit「…(431 の敵対的レビュー)」):

- テスト: 「language だけ写す」を `--settings` でなく引数の全体で比べる (ユーザー設定を `--effort` / `--permission-mode` に写す変更が全テスト緑で通っていた)。
  fixture に本物の settings.json の最上位の鍵を入れる。Start / Resume が組んだ引数をそのまま claude に渡すことを偽の claude で固定する
  (Resume の中で足す・組まずに渡す変更が通っていた)。% をセントに丸めてから割ることを固定する
- 名前: `persistentRoleSettings` → `persistentSessionSettings` (role.go の role = PM・取り込みの係と紛れる)、`quotaReadRate` → `fiveHourReadRate`、単価の表を鍵つきに
- コメント: テストが固定したことの言い直しを消し、理由の段落を launcher.go 冒頭の 1 か所に寄せた。dispatcher.go の Launcher の説明の取り残し (PG だけ・短い id は未実測) を直した。
  stats の USD が、単価の表を直す前に閉じた行と束ねると混ざることを罠として書いた
- issue: 計測の結論の誤り 3 つ (PG の 2.26M は下限で大小を比べられない / 546 の実験の外れの種類 / 再開 2/2 を効き目の証拠にした) と、数・言葉の細かい誤りを直し、431 を pending/ へ移した

記録して直さないもの:

- FiveHourPct の新旧のビット単位の一致は、arm64 で 5 分の書き込みが 1 model あたり約 3.6e15 トークンを超えると FMA の融合の違いで崩れる (現実の大きさの乱数 6 万件では一致)。
  入力の単価が 2 進で有限に表せない model (0.8 など) を表に足すと現実の大きさで効くので、そのときは明示の float64() 変換で丸めを固定する
- USD のセントへの丸めは、ちょうど半セントの額の約 5.7% で切り下げになる (旧版と同じ。差は 1 セント以下)
- `pro-con stats` の USD の新旧混在は、行に単価の表の版を持たせれば分けられるが、記録の形を変えるので入れない (stats.go の注記だけ)

2 周目 (1 周目の修正を攻め口にした 2 体): コード 8 件・issue の記述 12 件。採った修正:

- テスト: fixture の入れ子の値を本物と同じ形に (中身のある hooks・env の鍵・permissions の deny / ask)、止めてから再開する経路でも claude が受け取った引数を固定、
  役・名前をまたいで session の形を比べる (-n の値だけ伏せる)、flag が 1 回だけあることを見る、起動と再開で環境変数を比べる、偽の claude の記録を NUL 区切りに
  (改行を含む指示・「-」で始まる本文を通す)、厳密なテストの名前を本番の形に、% の丸めを 2 model で固定。どれも名指しの変異 (17 本) で red を確かめた
- issue: 再開の側の `"off"` は間接に観測できていた (上の 3。1 周目の修正で「試せていない」と書いたのは誤り)、データを守る手順を「dogfooding の前に schedule off」に、
  581 の中央値と倍率を 11 回・7 回で数え直し、数え方に書庫と events を足し、その他の数・言葉の誤りを直した

打ち切り: テストの脅威モデルは「pro-con のコードの変更で、起動と再開・役・名前の間で claude に渡す引数や環境変数が食い違う形と、ユーザーの設定を写す形」
(TestPersistentSessionProfile の冒頭。検出しないものも同じ所に書いた)。2 周目の指摘はこの範囲の迂回を 1 段ずつ深く突いたもので、全部を検査にして変異で閉じた。
例で守るテストは迂回が尽きないので、3 周目は回さない (production の判定ロジックは 1 周目から変えておらず、2 周目の修正は検査の追加と文面だけで、どれも直接の実測で確かめた)

### 2026-10-02 619 (Claude Code の mods) との関係

mods (関数 hook の plugin) は `--setting-sources` では外れず、プロセスの環境の `CLAUDE_CODE_PLUGIN_DIRS` からも読まれる。619 で dispatcher が役を起こす環境から
この変数を落とした (`roleEnv`。起動・再開・起動し直しは同じ `runClaude` を通るので揃ったまま。TestPersistentSessionProfile に親の環境に変数がある形を足した)。
mods は PG に載らないので tools は変わらず、残りの計測 (10 起動 / 10 再開) を mods の後の形で数え直す必要は無い。役に mods を載せると決めたら、そのときに数え直す

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
