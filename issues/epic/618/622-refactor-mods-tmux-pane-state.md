# 622 (refactor): tmux のペインの状態表示 (`tmux-pane-state.sh`) を mod へ移す

> 🚨 **担当中: Claude code mods migration design (epic 618 を順に)**（2026-10-02〜）

起票日: 2026-10-02

epic [618](618-design-claude-code-mods-migration.md) の子。619 の後。

## 概要

`_claude/hooks/tmux-pane-state.sh` (217 行) は 6 種のイベント (SessionStart / UserPromptSubmit / PostToolUse / Notification / Stop / SessionEnd) で
tmux の pane option `@claude_state` を書く。PostToolUse にも配線されているので、**ツール呼び出しのたびに bash が 1 本起動する**。
また Notification の stdin に `background_tasks` が無いため、Stop が書いた `@claude_bg` を Notification が pane option 経由で読む工夫をしている (script 冒頭)。

mod なら状態を常駐側に持て、`turn.start` / `turn.complete` / `classic.Notification` / `session.end` で書き換えられる。
→ このマシンでは `classic.*` が mod に届かない (619)。Notification は settings の hook に残り、mod が持てるのは `turn.start` / `turn.complete` / `tool.call` /
`session.start` / `session.end` の経路だけ (下の「go / no-go」)。以下の対応方針は、その前提で読み替える (3 と 5 は下で直した)

## 対応方針

**この issue は go / no-go から始める。** 今の script は動いていて、`tests/claude/test_tmux_pane_state_bell.sh` が条件を守っている。
移す利点は速度だけで、未計測。

1. **先に before を測る** (`perf-claims-need-measurement.md`): 1 ターンあたりの hook の起動回数と所要時間 (ツール呼び出しの多いターンで)。
   **移す条件を測る前に決めて本文に書く** (例: 1 ターンで体感できる遅れ = 数百 ms 以上を足しているなら移す)。条件に届かなければ移さずに閉じる
   (618 の表の「保留」を「移さない」に直す)
2. `_claude/mods/tmux-state/`: tmux への書き込みは `$.process.run` で `tmux set-option -p ...` を呼ぶ。
   tmux の実体は `command -v` で解決した絶対パスを使う (`bin/tmux` の shim を通すかは、shim は kill 系 (`kill-server` / `kill-session`) 以外を素通しするので、どちらでもよい。理由を書いて決める)
3. ~~bg 待ちの判定 (`@claude_bg`) は mod のメモリで持つ~~ → Notification の hook (settings に残る) が `@claude_bg` を pane option で読むので、pane option に書き続ける。
   表示側 (`_tmux.conf` / `tmux_agent_panel.sh` / `tmux_agent_jump.sh`) が読む pane option の値と形は変えない
4. macOS の通知とベルの条件 (ペインが見えていないときだけ) は今の script の判定をそのまま移す。`tests/claude/test_tmux_pane_state_bell.sh` が守っている条件を、mod のテストに写す
5. ~~切り替えの commit で settings の 6 行を外す~~ → mod に移した経路の行だけを外す (Notification の 1 行は残す)

## 確かめること

- [ ] after を before と同じ方法で測った
- [ ] ~~`classic.Notification` の `e` に `notification_type` が来ることを実測した (型には在る)~~ → このマシンでは届かない前提なので不要 (Notification は settings に残す)
- [ ] 隔離した tmux サーバ (`-L`) で、working / input / idle / bg の 4 状態の表示を確かめた (`tmux-probe-requires-socket-isolation.md`)
- [ ] `$.process.run` で起こした tmux に、そのペインの `TMUX` / `TMUX_PANE` が渡ることを確かめた (渡らなければ別のペインに書く)
- [ ] mod が読まれない経路 (619 の結果) で、表示が止まらないこと (その経路は settings の hook を残す)
- [ ] `session.end` の dispatch の時間の予算の中で `clear` が終わること

## go / no-go (2026-10-02)

**移す条件 (測る直前にこのセッションで決めた。起票時の例は「数百 ms 以上」)**: PostToolUse の経路の `tmux-pane-state.sh working` 1 回の所要の中央値 × 30 (ツール呼び出しの多いターン) が 300ms 以上なら移す。
🚨 この条件は今の費用を測るもので、移して減る量ではない (反証レビューの指摘)。下の after の見積もりで補う。

**before (実測)**: 隔離した tmux (`-L m622`) のペインの中から、本番のペインと同じ PATH で、PostToolUse と同じ stdin を渡して 40 回起こした。
min 25.0 / 中央値 28.7 / p90 43.9 / max 52.6 ms。中央値 × 30 = 約 860ms で、条件を超える。

**hook がターンの待ち時間に乗るか (実測)**: `claude -p --model haiku` に Bash を 3 回 (1 回ずつ) 使わせ、PostToolUse に `sleep 1` の hook を付けた腕と付けない腕を比べた。
`duration_ms - duration_api_ms` (API 以外の時間) が 653ms → 3,718ms で (各腕 1 回)、+3.07 秒 = 3 回 × 1 秒。PostToolUse の hook はツール呼び出しごとに直列で待たれる。
同じ PostToolUse に並ぶ他の hook (Bash なら `git-state-verify.sh` 等) と並列に走るかは測っていない。並列なら Bash のツールでは長い方に隠れるので、上の 860ms は上限として読む。

**after の見積もり (下限)**: mod は `tool.call` の後で `working` に戻すために tmux を 1 回起こす。同じ隔離したペインから tmux を直接起こす
(`set-option -p @claude_state … \; set-option -p -u @claude_bg`) と、40 回で min 5.8 / 中央値 6.9 / p90 7.5 / max 8.0 ms。
減る量の見積もりは (28.7 − 6.9) × 30 ≈ 650ms で、条件を超える。ただし `$.process.run` の起動の費用 (engine から子を起こす分) は含まない。
**判断: 条件付きで進める** — mod の試作で `tool.call` 1 回あたりの所要を測り、減る量が 300ms / 30 回を下回るなら移さずに閉じる。

**このマシンの制約 (619)**: user の mod には `classic.*` が届かない (managed settings のため `cc-plugin-sec-default` が bypass する)。
`classic.Notification` (input 状態と通知) は mod では受けられない前提になるので、Notification は settings の hook に残る。
mod に移せるのは `turn.start` / `turn.complete` / `tool.call` / `session.start` / `session.end` (いずれも届くことを実測) の経路だけ。
Notification の hook が書いた `input` を mod は知らないので、`tool.call` で `working` に戻すには、そのつど pane option を読むか書くかで tmux を 1 回起こす必要がある。

- ratelimit の裏の `claude -p /usage` (user の settings を読む) でも mods が走るはず (619 の敵対的レビュー。settings の env からの読み込みが実測されたら確定)。tmux の状態を書く mod は対話のセッションに絞る

## 進捗

- 2026-10-02: 起票
- 2026-10-02: 起票と同じ日に、反証レビュー (sonnet 2 本) と敵対的レビュー (opus 2 本) の指摘で方針を改訂した。622 を go / no-go (before の実測で移すか決める) に直し、`TMUX_PANE` が渡るか・`session.end` の予算の確認を足した。採否と理由の一覧は親 618 の進捗
- 2026-10-02: before を実測 (中央値 28.7ms × 30 = 約 860ms で、条件 300ms を超えた)。反証レビューで「条件は移して減る量ではない」と指摘され、tmux 直の下限 6.9ms で減る量を約 650ms と見積もった。`$.process.run` の費用を試作で測ってから決める (条件付きで進める)。PostToolUse の hook はターンの待ち時間に直列で乗る。ただし Notification は mod に届かないので settings に残り、mod は `tool.call` の後で `working` に戻すときに tmux を 1 回起こす形になる
