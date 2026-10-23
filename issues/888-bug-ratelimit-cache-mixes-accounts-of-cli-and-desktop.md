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
読み手の `src/ratelimit/usage/statusline.go` は「別のアカウントのセッションも同じファイルに書く。今は同じアカウントだけを使う前提
(pro-con の `dispatcher/usage.go` の注記と同じ)。別のアカウントを使うようになったら、ファイルを分ける」と前提を宣言している。
**その前提が崩れている** (CLI と desktop で別アカウント)。

## 詳細

### 観測 (2026-10-23 10:28〜10:40)

- 10:28 ごろ `ratelimit`: `claude 5h 82% (10月23日13:40) [4分前に取得]`
- 10:36 `ratelimit -json`: 5h 53%・resetAt 13:00 (fetchedAt 10:32)。`claude-rate-limits.json` は 56%・resets_at 13:00
  (writers.five_hour.by はこの session の transcript)、glogx の `claude-usage.json` は 55%・13:00
- 13:00 にリセットする窓と 13:40 にリセットする窓は、同じアカウントで同時に今の窓にはならない。82% / 13:40 は別の窓の値
- 10:42:44 にファイルが再び 82% / resets_at 13:40 になった。5h の書き手は `"by":"833e594a-f35f-4cef-8db5-8bbb74d76d76","version":""`。
  by が transcript のパスでなくセッション ID で、版が空なのは、desktop の mod の呼び出しの形 (`_claude/mods/desktop-statusline/hooks/input.ts` は
  `transcript_path: ''` を渡し、版を渡さない) と一致する。CLI の session の書き手は transcript のパスと `2.1.308` を残している。
  desktop 側のアカウントの値だと読める (desktop のアカウントの実際の 5h を desktop の画面で突き合わせてはいない)

### 混ざる仕組み (`_claude/statusline-command.sh` の `rl_newer` / `write_rate_limits`)

- 窓ごとに、リセット時刻が今のファイルより `rl_same_window` (600 秒) を超えて後ろの観測は「次の窓」として必ず採る。
  アカウント B の窓 (13:40) がアカウント A の窓 (13:00) より後ろにあると、B の値は A の値を無条件に置き換える
- `rl_fresh` (transcript がファイルより新しい) のセッションは窓を問わず採るので、CLI (A) の session が応答を受け取ると A の値に戻る。
  ただし desktop は transcript を渡さないので rl_fresh にならず、書けるのは「次の窓」(リセットが 600 秒を超えて後ろ) のときだけ。
  B の窓が後ろにある間は、desktop の 60 秒ごとの描画のたびに B が A を上書きし直す。**リセットが後ろの窓を持つアカウントが優勢**になる
  (10:36 には CLI の値 56% / 13:00 が載っていて、10:42 に desktop の値へ戻った)
- desktop が書いた行の `by` はセッション ID だけで、どのアカウント・どの経路かを示さない
- 読み手 (`src/ratelimit/usage/statusline.go`・`bin/ratelimit`・glogx・pro-con の dispatcher) はアカウントを区別しない

## 対応方針 (案。決めるのは設計の後)

- 書くときに出所 (アカウント・設定ディレクトリ・CLI か desktop か) を区別できる値を持たせ、読み手は自分のアカウントの値だけを読む。
  statusline の入力にアカウントを示す欄があるかは**未確認** (無ければ、設定ディレクトリや desktop の mod からの呼び出しの印で分ける)
- 最低限: desktop の mod から呼ばれたときは `write_rate_limits` を書かない (CLI のアカウントの判断に desktop の値を混ぜない)。
  desktop の表示は入力の `rate_limits` から描いていて (`statusline-command.sh` の `pace_row`)、このファイルを読まないので、表示は変わらない (反証レビューで確認)
- 観測: 採った・捨てた・入れ替えた観測を、書き手 (by・アカウントの印) とリセット時刻つきで 1 行ずつ残す (今は最後の書き手しか残らないので、外れ値の出所を後から追えない)

## 関連ファイル

- `_claude/statusline-command.sh` (`rl_newer` / `write_rate_limits`)
- `_claude/mods/desktop-statusline/hooks/register.tsx`
- `src/ratelimit/usage/statusline.go` / `src/ratelimit/README.md` (「Claude の枠の主な出所」)
- `_claude/hooks/ratelimit-warn.sh` / `_claude/mods/ratelimit-warn/`
- retro 833 (同じファイルで 7d=99% が張り付いた件。「同じ窓では高い方を残す」をやめた経緯)

## 進捗

- 2026-10-23 (commit「fix(ratelimit): desktop の利用枠を CLI と別のファイルへ書く (issue 888)」。ユーザー指示 2026-10-23「書き出し先は分離したい」):
  - [x] desktop の mod は入力に `surface: "desktop"` を渡し、`write_rate_limits` は surface が desktop か `CLAUDE_CODE_ENTRYPOINT=claude-desktop` のとき
    `claude-rate-limits-desktop.json` に書く。読み手 (ratelimit・hook・glogx・pro-con) は `claude-rate-limits.json` だけを読むまま (CLI のアカウントだけ)。
    ratelimit の module は分けていない (混ざっていたのは書き手だけ)
  - [x] テスト: `TestStatuslineDesktopWritesSeparateFile` (mod の印・desktop の環境の 2 経路。desktop の窓が後ろにある形で CLI の値が残ること)。mod の `pure.test.ts`。
    テストの書き手の起動は `CLAUDE_CODE_ENTRYPOINT` を外す (desktop の中で go test を回しても CLI の検査が desktop のファイルへ書かない)
  - 変異 (全部 red): script が印を見ない / 環境の印を見ない
  - 検証: ratelimit の go test -race・make lint 0 件・tests/claude/test_statusline.sh・tests/claude/test_claude_mods.sh (6 件)・make test-lint が緑
  - 敵対的レビュー 1 周 (sonnet): P1 / P2 なし。**記録 (P3)**: CLI が desktop の環境 (`CLAUDE_CODE_ENTRYPOINT=claude-desktop`) を継承したまま起動する形は未実測
    (CLI は起動時に自分の値で上書きすると推定) / 逆向き (CLI なのに desktop の印) のテストは無い / desktop のファイルには読み手が無い (今は観測のためだけに書く)
- 2026-10-23 起票。反証レビュー (sonnet 1 体・読み取りのみ): P2 desktop は rl_fresh にならない (transcript_path が空) ので「最後にやり取りした方」ではなく「リセットが後ろの窓の方」が優勢 → 直した /
  P3 読み手の注記 (同じアカウントだけの前提) に触れていない → 足した。反証できなかった: desktop の mod が同じ script を rate_limits 付きで呼ぶ・desktop の分岐が無い・
  読み手がアカウントを区別しない・600 秒の判定・desktop の表示がファイルを読まない
