# 621 (feat): human / retro の催促を、mod でプロンプトの上の帯に出す

> 🚨 **担当中: Claude code mods migration design (epic 618 を順に)**（2026-10-02〜）

起票日: 2026-10-02

epic [618](618-design-claude-code-mods-migration.md) の子。619 の後。

## 概要

`human-tasks-due.sh` と `retro-open.sh` は SessionStart で件数・期限切れを注入し、規約 (`_claude/issue-rules.md`) が
モデルに「冒頭で一言伝える」をさせている。届け先は人なのに、モデルを経由し、文脈も使っている。

mod の `AbovePrompt` の帯なら、人へ直接出せる。

## 対応方針

- `_claude/mods/issue-band/`: `ui.render` の `{ component: 'AbovePrompt' }` に、期限切れ / 期限の近い human と、未決着の retro の件数を 1 行で出す。
  0 件なら `next(e)` (何も出さない)
- 数え方は今の hook と同じにする。走査の対象ディレクトリ (`issues/` 直下・`pending/`・`next/`・`epic/*/` …) は
  `_claude/hooks/lib/issue-hooks.sh` が持っているので、**数え方を mod に写さない**。`$.process.run` で既存の script を呼んで結果を読む形を第一案にする
  (TS で書き直すと 2 実装になる)
- 更新のきっかけ: `session.start` と `turn.complete` (issue を done へ移した直後に減るように)。そこで script を呼んで結果を `$.state` に置き、
  `ui.render` は state を読むだけにする (描画の dispatch の中で外部の script を呼ばない。dispatch には時間の予算がある)
- ~~620 の後に着手する (どちらも `_claude/issue-rules.md` の文面を直す)~~ → 620 は文面を変えずに閉じたので外れた (下の「前提の実測」)

## 失敗モード

帯は mod が読み込まれないと黙って消える。`retro-open.sh` の冒頭は「誰も読まなければ永久に open のまま溜まる」を既定の壊れ方としている。
今はモデルが冒頭で伝えるので、hook が壊れても規約の文が補っている。

- 帯へ移しても、しばらくは SessionStart の 2 本と規約の「冒頭で一言伝える」を残して併記する。外すのは、619 の実測と、
  帯が出なかったときに気づく手段 (620 の「気づく手段」と共通にできる) が揃ってから
- 外すかどうかの判断と、その時点の根拠を本文に書く

## 決めること

- [ ] `ratelimit-warn.sh` (5h 枠) と `next-claim-unshared.sh` (他マシンから見えない claim) の情報も帯に足すか。
      どちらもモデルに行動 (提案 / push の伺い) をさせるための注入なので、**注入は残し、帯は人向けの表示として足すだけ**にするのが第一案
- [ ] 帯の幅が狭い端末での見え方。決める前に見本を出す (`decide-layout-in-sample-renderer-first.md`)

## 前提の実測 (2026-10-02 / claude 2.1.287。619)

- このマシン (managed settings あり) でも、user の mod に `session.start` / `turn.complete` / `ui.render` (AbovePrompt) / `$.ui.status` は届く。
  対話 (隔離した tmux) で probe の mod の帯 (`EVPROBE-BAND`) がプロンプトの上に描かれ、status にも出た
- 620 は「移さない」で閉じ、`_claude/issue-rules.md` の文面は変えていないので、「620 の後に着手」の条件は外れた
- ratelimit の裏で起きる `claude -p /usage` (user の settings を読む) でも mods が走るはず (settings の env からの読み込みが 619 で実測されたら確定)。帯の mod は `session.start` の `e.isInteractive` / `e.surface` で絞り、
  非対話のセッションでは script を呼ばない
- 帯の見本 (4 案) をユーザーに出した (2026-10-02)。推奨は案 3 (人がやる必要があるとき = 期限切れ・期限が近い・retro 未決着のときだけ出し、余裕のある human だけなら出さない)。返事待ち

## 進捗

- 2026-10-02: 起票
- 2026-10-02: 起票と同じ日に、反証レビュー (sonnet 2 本) と敵対的レビュー (opus 2 本) の指摘で方針を改訂した。621 に、帯が黙って消える失敗モード (併記の期間を置く)・描画は `$.state` を読むだけ・620 の後に着手、を足した。採否と理由の一覧は親 618 の進捗
- 2026-10-02: 前提を実測 (帯と status は mod から描ける)。見本 4 案を出して見た目の返事待ち。件数は script に機械向けの出口 (`--counts` 等) を足して mod から呼ぶ方針
