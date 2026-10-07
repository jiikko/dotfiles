# 621 (feat): human / retro の催促を、mod でプロンプトの上の帯に出す

起票日: 2026-10-02

epic [618](../618-design-claude-code-mods-migration.md) の子。619 の後。

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
- ratelimit の裏で起きる `claude -p /usage` (user の settings を読む) でも mods が走る (settings の env から `-p` も読むことを 619 で実測)。帯の mod は `session.start` の `e.isInteractive` / `e.surface` で絞り、
  非対話のセッションでは script を呼ばない
- 帯の見本 (4 案) をユーザーに出した (2026-10-02)。推奨は案 3 (人がやる必要があるとき = 期限切れ・期限が近い・retro 未決着のときだけ出し、余裕のある human だけなら出さない)。返事待ち

## 実装 (2026-10-02)

- 見た目はユーザーが案 3 を選んだ (2026-10-02): 期限切れ (赤)・期限が近い (黄)・期限の読めない (黄)・retro 未決着 (シアン) を ` · ` で並べ、どれも 0 なら帯を出さない (余裕のある human だけなら出ない)。
  見本に無かった「期限の読めない human」(script の broken = 期限なし・書式不正) は人が直す必要があるので足した。狭い端末では各段を `wrap="truncate"` で切る
- `_claude/mods/issue-band`: 対話のセッションでだけ、`session.start` と (メインの) `turn.complete` で `human-tasks-due.sh --counts` / `retro-open.sh --counts` を
  呼んで `$.state` に置き、`ui.render` (AbovePrompt) は読むだけ。`turn.complete` の数え直しは待たずに裏で走らせ (script は dotfiles で約 0.6 秒)、サブエージェントのターンでは数えない。
  script の場所は `$.plugin.root` の 2 段上の `hooks/`。script が失敗・非 0 で終わったときも、数えられなかった理由を帯に出す
- テスト: `claude plugin test` 16 本 (件数の読み取り・帯の組み立て・script の場所と cwd・非対話では呼ばない・terminal と desktop で描く / 出さないときは engine に任せる・
  survey に譲る・失敗の理由を出す・ターンの終わりに数え直す・サブエージェントでは数えない)。変異 7 本で red を確認。
  「出さない」のテストは最初は文字が無いことしか見ておらず空の帯を見逃したので、engine の描画が残ることを見る形に直した
- 敵対的レビュー (sonnet) の指摘で直した: サブエージェントのターンでも script を待っていた / 対話かどうかを module の変数に持っていた (読み直しで消える。毎回 `$.session.surfaces()` で聞く形に) /
  テストの抜け 4 つ / script が非 0 で stdout が空だと帯が黙って消える。採らなかった: script と mod の間のキー名を検査で結ぶこと (どちらのテストも今の形を固定しており、ずれても数えられない帯ではなく 0 件に倒れるだけ)
- **本物の画面で確認した**: trust 済みの `~/src/slack-cli` (期限切れの human 1 件・retro 1 件) で、mod だけを `--plugin-dir` で載せた対話のセッションを隔離した tmux で起こすと、
  プロンプトの上に `期限切れの human 1 件 · retro 未決着 1` が出た (model は呼んでいない)

## 決めたこと (2026-10-02)

- SessionStart の `human-tasks-due.sh` / `retro-open.sh` の注入と、規約の「冒頭で一言伝える」は残して併記する。帯が出なかったときに気づく手段がまだ無いため (外すときはこの節を書き直す)
- `ratelimit-warn.sh` / `next-claim-unshared.sh` の情報は帯に足さない。どちらもモデルに行動させるための注入で、帯に出しても役目を果たさない (第一案のとおり)

## 進捗

- 2026-10-02: 起票
- 2026-10-02: 起票と同じ日に、反証レビュー (sonnet 2 本) と敵対的レビュー (opus 2 本) の指摘で方針を改訂した。621 に、帯が黙って消える失敗モード (併記の期間を置く)・描画は `$.state` を読むだけ・620 の後に着手、を足した。採否と理由の一覧は親 618 の進捗
- 2026-10-02: 前提を実測 (帯と status は mod から描ける)。見本 4 案を出して見た目の返事待ち。件数は script に機械向けの出口 (`--counts` 等) を足して mod から呼ぶ方針
- 2026-10-02: 数え方の出口を足した — `human-tasks-due.sh --counts` (`overdue= soon= human= broken=`) / `retro-open.sh --counts` (`retro= held= odd=`)。0 件でも必ず出し、issue dir が無ければ何も出さず、lib を読めなければ `error=` を出す。テストに 7 観点 (変異 5 本で red を確認)。帯の mod は `_claude/mods` に置くと push した時点で全セッションに載るので、見た目の合意の後に入れる
- 2026-10-02: 帯の mod を実装 (案 3)。敵対的レビューの指摘を直し、テスト 16 本・変異 7 本。本物の画面でも確認した。注入は併記で残す。受け入れの残りが無いので done
