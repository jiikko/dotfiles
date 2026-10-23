# 888 (bug): CLI と desktop で別アカウントのとき、利用枠のキャッシュに別アカウントの値が入り、ratelimit が誤った 5h を出す

起票日: 2026-10-23

## 概要

2026-10-23 の 1 つの session で、UserPromptSubmit の注入 (`ratelimit -source claude -check -cached`) が
「claude 5h 82% (13:40 にリセット)」を約 1 時間出し続けた。同じ時刻のサーバの値は 53〜56% (13:00 にリセット) で、
ユーザーは枠が逼迫していると読んで作業のペースを下げた。

ユーザーは CLI (Claude Code) と GUI (Claude の desktop) で別のアカウントにログインしている (ユーザー 2026-10-23)。
desktop の statusline の mod も、CLI と同じ `_claude/statusline-command.sh` を `rate_limits` 付きで呼ぶ
(`_claude/mods/desktop-statusline/hooks/register.tsx`・`tests/register.test.tsx` の「CLI と同じ形の JSON で呼ぶ」)。
`write_rate_limits` はどちらから呼ばれても同じ `~/.cache/glog/claude-rate-limits.json` に書くので、2 つのアカウントの窓が 1 つのファイルで混ざる。

## 詳細

### 観測 (2026-10-23 10:28〜10:40)

- 10:28 ごろ `ratelimit`: `claude 5h 82% (10月23日13:40) [4分前に取得]`
- 10:36 `ratelimit -json`: 5h 53%・resetAt 13:00 (fetchedAt 10:32)。`claude-rate-limits.json` は 56%・resets_at 13:00
  (writers.five_hour.by はこの session の transcript)、glogx の `claude-usage.json` は 55%・13:00
- 13:00 にリセットする窓と 13:40 にリセットする窓は、同じアカウントで同時に今の窓にはならない。82% / 13:40 は別の窓の値
- 82% / 13:40 を書いたセッションは特定できていない (writers が今の値で上書きされた後に調べた)。desktop 側のアカウントの値だというのは**推測**

### 混ざる仕組み (`_claude/statusline-command.sh` の `rl_newer` / `write_rate_limits`)

- 窓ごとに、リセット時刻が今のファイルより `rl_same_window` (600 秒) を超えて後ろの観測は「次の窓」として必ず採る。
  アカウント B の窓 (13:40) がアカウント A の窓 (13:00) より後ろにあると、B の値は A の値を無条件に置き換える
- `rl_fresh` (transcript がファイルより新しい) のセッションは窓を問わず採るので、A の session が応答を受け取ると A の値に戻る。
  どちらの値が載るかは、最後にどちらのアカウントでやり取りしたかで決まる
- 読み手 (`src/ratelimit/usage/statusline.go`・`bin/ratelimit`・glogx・pro-con の dispatcher) はアカウントを区別しない

## 対応方針 (案。決めるのは設計の後)

- 書くときに出所 (アカウント・設定ディレクトリ・CLI か desktop か) を区別できる値を持たせ、読み手は自分のアカウントの値だけを読む。
  statusline の入力にアカウントを示す欄があるかは**未確認** (無ければ、設定ディレクトリや desktop の mod からの呼び出しの印で分ける)
- 最低限: desktop の mod から呼ばれたときは `write_rate_limits` を書かない (CLI のアカウントの判断に desktop の値を混ぜない)。
  desktop の表示には今の入力の `rate_limits` をそのまま使えるので、ファイルを経由しなくても表示は変わらない (未確認)
- 観測: 採った・捨てた・入れ替えた観測を、書き手 (by・アカウントの印) とリセット時刻つきで 1 行ずつ残す (今は最後の書き手しか残らないので、外れ値の出所を後から追えない)

## 関連ファイル

- `_claude/statusline-command.sh` (`rl_newer` / `write_rate_limits`)
- `_claude/mods/desktop-statusline/hooks/register.tsx`
- `src/ratelimit/usage/statusline.go` / `src/ratelimit/README.md` (「Claude の枠の主な出所」)
- `_claude/hooks/ratelimit-warn.sh` / `_claude/mods/ratelimit-warn/`
- retro 833 (同じファイルで 7d=99% が張り付いた件。「同じ窓では高い方を残す」をやめた経緯)

## 進捗

- [ ] 未着手
