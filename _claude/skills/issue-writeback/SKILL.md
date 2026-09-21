---
name: issue-writeback
version: 1.0.0
description: このセッションで関わった issues/ の .md に更新漏れが無いかを点検し、あれば本文へ書き戻す。「issue の更新漏れない?」「issue 更新した?」「書き戻し漏れ」「/issue-writeback」で発火。完了検出と done/ への移動は issue-sync の担当で、この skill はファイルを動かさない。
---

# Issue Writeback

このセッションでやったことが、関わった issue の**本文に書き戻されているか**を点検し、漏れていれば書く。
**ファイルは動かさない**（完了検出と `done/` への移動は `issue-sync`）。

## なぜ手動の skill が要るか（Stop hook があるのに）

Stop hook (`_claude/hooks/issue-progress-check.sh`) が同じ関心で出口を見張っているのに、実運用では
漏れ続けてユーザーが毎回手でリマインドしている。hook のヘッダと実装が理由を自分で述べている:

1. **構造しか見ない** —「中身の正しさは判定しない」と明記。`[x]` か見出しか番号への言及が 1 つ増えれば
   黙るので、**実態と食い違う本文・古いままの記述はそのまま通る**
2. **未 commit で番号も出していない作業を検出しない**（同じくヘッダに明記）。commit subject の `(NNN)` /
   変更パス / `next/` の claim が入口なので、そのどれにも現れない作業は入口ごと無い
3. **1 セッションに同じ行を 1 回しか出さない**。しかもターンの最後に block として届き、
   「更新不要」と一言返せば閉じられる。**その後に触った分は二度と再指摘されない**

この skill は **hook の出力に依存せず、その時点の全量を自分で見る**。hook は出口の網、こちらは
人が呼ぶ点検で、**網が黙った / 流された分**を拾うのが仕事。

🚨 **`issue-progress-check.sh` を手で叩いて報告を作らない**。`~/.cache/claude-issue-progress/<session>.reported`
に既読印を書くので、**本番の Stop 判定をそのぶん無音にする**。

## 適用条件

`issues/`（または `issue/`）を持つ repo だけ。無ければ「この repo は issues/ を持たないため対象外」と
1 行報告して終了する（黙って終わらない）。

## Step 1: 対象集合を作る（一次情報は会話履歴）

**まず会話履歴を読む**。このセッションで何を直し、何を測り、何を却下したか。git から出る番号は補助。
以下の和集合を取る:

- 会話中に**番号を挙げた / 本文を読んだ / 作業の根拠にした** issue（commit していなくても対象）
- `git worktree list --porcelain` の**各 worktree**で `git status --porcelain --untracked-files=all` /
  `git diff --name-only` / `git log --format=%s`（worktree の commit は本体からは見えない）
- `issues/.../next/` に置かれた claim（symlink）

issue dir の解決と固定 2 段（`epic/<name>/` とその状態ディレクトリ）の走査は
`_claude/hooks/lib/issue-hooks.sh` の `issue_hook_resolve_dir` / `ISSUE_HOOK_DIRS` に揃える。
`find` を自作するなら **`-name done -prune`** と「**`next/` の symlink は `-type f` で数えない**」を落とさない。

🚨 **「commit していないから対象外」にしない**。hook が構造的に落とすのがまさにここで、
この skill の存在理由の半分を占める。

## Step 2: 判定基準は正本を読む

`~/dotfiles/_claude/issue-rules.md` の「**本文**」節を Read し、その要求で判定する。
**この SKILL.md に基準を写さない**（写したコピーは正本の改訂が伝播せず、古いまま残る）。

正本に無い、**セッション由来だから機械には見えない**観点を足す:

- **やったこと ↔ 本文**: 実装・検証・実測値・却下の結論が本文に無いか。本文の「現状は〜」が今も正しいか
- **`./tmp` に出したレポート**: 結論・全数勘定・**却下理由**が issue へ移っているか
  （移していなければセッション終了で消える）
- **retro**: 実質的な作業をやり切ったなら起票したか。既存 retro の残課題が今日決着したなら本文へ
- **human**: `期限:` が**行頭 + 半角コロン**の書式か（箇条書き `- 期限:` / 太字だと hook が黙って取りこぼす）
- **関連 open issue**: その番号を参照している open issue に「NNN で解消 / 継続」が要らないか

## Step 3: 報告する

対象 issue ごとに 1 行。**漏れが無いものも「漏れなし」と明示する**（無報告と未点検は区別できない）。

```
## issue 書き戻し点検: 対象 N 件 / 要追記 M 件

| issue | 判定 | 何が足りないか |
|---|---|---|
| 407 | 要追記 | 変異検証の結果 (red 1 本) が本文に無い |
| 410 | 漏れなし | — |
```

## Step 4: 書き戻す

- 検出したら**同じターンで追記する**。「次に書きます」でターンを終えない
- **更新不要と判断した issue は、その理由を 1 行残す**（黙ってスキップしない）
- 追記できたら pathspec で commit する
- **`done/` へ移すべきものを見つけても、この skill では動かさない**。「issue-sync 相当の判断が要る」と
  報告に書いて回す（状態遷移の判断を 2 つの skill に分散させない）

## 関連

- `~/.claude/skills/issue-sync/SKILL.md` — **境界**: あちらは「完了しているのに open なもの」を探して
  `done/` へ移す（状態の遷移）。こちらは「open のまま本文が実態に追いついていない」を直す（本文の内容）
- `~/dotfiles/_claude/issue-rules.md`「本文」節 — 判定基準の正本
- `_claude/hooks/issue-progress-check.sh` — 出口の網。**読むのはヘッダ（何を検出しないか）まで**で、実行はしない
