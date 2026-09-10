# human: bg タスク実行中にベルが出ないことを実機で確認する

起票日: 2026-09-09
カテゴリ: human（人間しかできない動作確認）
期限: 2026-09-16
出典: [retro 336](done/336-retro-bell-suppression-bg-2026-09-08.md) の残課題「人の動作確認」

## 何を確認してほしいか

**バックグラウンドタスクを走らせたまま入力待ちにして、tmux のウィンドウにベル（🔔）が
出ないこと**。逆に、bg が無い状態の入力待ちでは**出ること**。

unit テストは隔離 tmux サーバで hook の判定までを固定しているが、
**実際の Claude Code → hook → tmux の経路は hook 経由でしか再現できない**ため未確認。

## 手順

1. tmux の中で Claude Code を起動する
2. 何か長い処理を **background で** 走らせる（例: `make test` を `run_in_background`）
3. その状態で Claude が入力待ち（許可を求める / 質問する）になるようにする
4. **tmux のウィンドウ名にベル（🔔）が付かないこと**を見る
   - 付かなければ ✅。ステータスは `⚙ working (bg:N)` 相当になっているはず
5. background の処理が終わってから、もう一度入力待ちにする
6. **今度はベルが付くこと**を見る（← ここが出ないなら、抑制が効きすぎている）

## 期待

| 状態 | ベル |
|---|---|
| bg タスクあり + 入力待ち | **出ない** |
| bg タスクなし + 入力待ち | **出る** |
| bg タスクなし + タスク完了 | **出る** |

依頼時の要望は「バックグラウンドタスクがなくて、本当に人間の入力待ちやタスクが完了した時は、
ベルを出してほしい」なので、**5〜6 が出ない方が問題**（見逃しは人が来るまで誰も直せない）。

## 結果の書き方

確認できたら、このファイルを `issues/done/` へ移す（既読はファイルの位置で表す）。
**期待と違ったら**、どちらの向きに違ったか（出るべきで出ない / 出ないべきで出る）と、
そのときの `tmux show -p -v -t <pane> @claude_state` / `@claude_bg` の値を本文に足してから
`issues/` に残す。

## 関連

- [retro 336](done/336-retro-bell-suppression-bg-2026-09-08.md)
- `_claude/hooks/tmux-pane-state.sh` — 判定の実体（`bg_waiting` / `@claude_bg`）
- `tests/claude/test_tmux_pane_state_bell.sh` — unit の回帰（Test 5 / 6 / 6b / 6c）

## 2026-09-10: 期待表 3 行はすべて**実 tmux サーバで機械検証済み**だった（残りは 1 点）

`tmux-probe-requires-socket-isolation.md` の「human に回す前に隔離 `-L` サーバで測れないか一度問う」
を当てた。**テスト名から推測せず本文を読んで確認している。**

| 期待表の行 | 覆っているテスト | 中身 |
|---|---|---|
| bg あり + 入力待ち → **出ない** | `test_tmux_pane_state_bell.sh` **Test 6b** | `idle_prompt` と `permission_prompt` の両方で `window_bell_flag` が 0。かつ状態が `⚙ working (bg:1)` のままであることも固定 |
| bg なし + 入力待ち → **出る** | **Test 5** | `idle_prompt` + `permission_prompt` / `elicitation_dialog` / `elicitation_url_dialog` / `agent_needs_input` / `worker_permission_prompt` / `quota_auto_resume_disabled` の **7 種別**を 1 つずつ pin |
| bg なし + タスク完了 → **出る** | **Test 6c** | 🚨 **この issue が「出ない方が問題」と名指ししている側**。`working` を挟むと bg フラグが落ちてベルが戻る／`bg なしの Stop` でも落ちる、の**2 経路**を別々に固定（後者は `bell_flag` では判定できないので状態で見ている、という注記つき） |
| （配線） | **Test 1** | `_claude/settings.json` にフックが配線されていること。外れたら「誰も鳴らさない」になるので |

判定はどれも**隔離した実 tmux サーバの `window_bell_flag`** で、モックではない。

### 本番側の生きた証拠（read-only で観測。サーバは一切作っていない）

```
$ tmux list-panes -a -F '… bg=#{@claude_bg} bell=#{window_bell_flag} state=#{@claude_state}'
pj_energy_matching:7.1  bg=2  bell=0  state=_ working (bg:2)     ← 実 Claude が bg 2 本を抱えている
（99 ペイン中 @claude_state を持つのは 5 ペイン。残り 94 は非 Claude ペイン）
```

**hook は本番で実際に動いており、`@claude_bg` の計数も効いている**。

### 🚨 残っているのは「実 Claude の payload が想定どおりか」だけ

unit テストが与えているのは**合成 JSON**なので、最後に残る不確かさは 2 つ:

1. 実 Claude Code が `Notification` を **想定した `notification_type`** で送ってくるか
   （テストは「claude 2.1.263 のバイナリが持つ enum 14 値」から導いたと本文に書いてある）
2. `Notification` の stdin に `background_tasks` が**無い**という前提（だから `Stop` が書いた
   `@claude_bg` を読む設計になっている）が、今の版でも成り立つか

これは**目視でも確かめられない**（人が見えるのは 🔔 の有無だけで、payload は見えない）。
確かめるなら hook 側に観測を足すか、本番ペインの 3 変数 `@claude_state` / `@claude_bg` /
`window_bell_flag` の**遷移**を read-only で記録する。

### 提案（ユーザー承認待ち）

**本番サーバへの read-only ポーリングで遷移ログを取る**。`tmux list-panes -F` の読み取りだけで、
サーバもセッションも作らない。取れたら期待表を機械判定できるので、この issue は human から外せる。
🚨 **`window_bell_flag` はウィンドウを表示した時点でクリアされる**ので、サンプリング間隔より短い
`0→1→0` は取りこぼす（「取りこぼしうる」前提で読む必要がある）。

**承認が要る理由**: 依頼の範囲外で本番 tmux サーバへ常駐のポーリングを足すことになるため。
read-only だが、勝手に足すものではない。

### 現時点で人に残る作業

- [ ] 上のポーリングを承認するか、または従来どおり 🔔 を 1 回目視する（手順は上の「手順」節）

**期待表の 3 行そのものは機械で覆われている**ので、目視するとしても「実 Claude の payload が
想定どおりに流れているか」の 1 点だけでよい。

## 残タスク 2026-09-10: 前提 2 つのうち **1 つを実測で潰した**

前節が「残るのは実 Claude の payload が想定どおりか」として 2 点を挙げていた。そのうち **①を潰した**。

### ① `notification_type` の enum → **現行版でも 14 値が完全一致**（✅ 解消）

テストの denylist / allowlist は **claude 2.1.263** のバイナリから導いたと書かれていたが、
**いま走っているのは 2.1.266**（`claude --version`）。版が動いていたので数え直した。

`2.1.266` の実体（`…/24.2.0/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`、
Mach-O arm64 199MB）から取れた enum:

```
["permission_prompt","idle_prompt","auth_success","elicitation_dialog","agent_needs_input",
 "agent_completed","elicitation_url_dialog","worker_permission_prompt","push_notification",
 "computer_use_enter","computer_use_exit","quota_auto_resume_fired","quota_auto_resume_stale",
 "quota_auto_resume_disabled"]
```

**14 値、テストが前提にしている集合と 1 つも違わない。** 個別在否も 14/14 で「在り」。
→ テストの allowlist（人が要る 7 値）/ denylist（要らない 7 値）は**現行版でも有効**。

🚨 **測る対象を 1 度間違えた**。`find` で最初に当たった `cli.js` は
**19.3.0 配下の 2.1.17**（古い install）で、そこで数えると enum は
`["permission_prompt","idle_prompt","auth_success","elicitation_dialog"]` の **4 値**しか無い。
そのまま報告していたら「enum が縮んだ」という**逆の結論**になっていた。
nodenv 配下に **3 つの版が同居**している（24.2.0=2.1.266 / 22.11.0=2.1.263 / 19.3.0=2.1.17）ので、
版を確かめずに測ってはいけない。

### ② `Notification` の stdin に `background_tasks` が無いか → **未実測**

`strings` では schema の形（どのイベントがどのフィールドを持つか）まで判別できなかった
（`background_tasks` という文字列は在るが、それは `Stop` / `SubagentStop` 用でも同じこと）。
**「無い」も「在る」も示せていない**ので未実測のまま残す。

確かめるなら、本文が挙げている 2 案のどちらか:
- hook 側に観測を足して実 payload を記録する
- 本番ペインの 3 変数（`@claude_state` / `@claude_bg` / `window_bell_flag`）の遷移を
  read-only で追記ログに取る ← **別セッションが用意中。実行にはユーザー承認が要る**

### 残っている確認

- [x] 期待表 3 行 = 隔離した実 tmux サーバのテストが覆っている（Test 5 / 6b / 6c）
- [x] hook の配線（Test 1）と、本番ペインで実際に動いていること（read-only 観測）
- [x] **`notification_type` の enum が現行版 2.1.266 でも一致**（この節）
- [ ] `Notification` に `background_tasks` が無いという前提（**未実測**。上の 2 案のどちらか）
