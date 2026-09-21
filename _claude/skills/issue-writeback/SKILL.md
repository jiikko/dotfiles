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

issue dir の解決（`issues/` と `issue/`、`<root>/*/issues` の入れ子 1 段）は、**dotfiles がある環境では
既存の lib に揃える**。hook JSON は要らず、stdin を空にすれば cwd から解決する（実測 2026-09-22）:

```sh
bash -c '. ~/dotfiles/_claude/hooks/lib/issue-hooks.sh && issue_hook_resolve_dir </dev/null &&
  printf "ROOT=%s\nDIRS=%s\n" "$ISSUE_HOOK_ROOT" "$ISSUE_HOOK_DIRS"'
```

🚨 **`bash -c` で包む（zsh から直接 source しない）**。この lib は `<root>/*/issue` を glob で試すので、
zsh では**無マッチが NOMATCH でコマンドごと落ちる**（実測 2026-09-22: `no matches found` + rc=1）。
落ち方が最悪で、**rc=1 が「issue dir が無い」と区別できない**ため、issues/ を持つ repo で
「対象外」と誤報して黙る。

🚨 **dotfiles が無い環境（他人のマシン・CI）ではこのパスは存在しない**。その場合は自分で走査する。
どちらの経路でも落としてはいけないのは 3 つ:

- **`-name done -prune`**（`-path` で書くと起点の綴りで一致しなくなり done 全件が open に化ける）
- **`next/` の symlink を `-type f` で数えない**（claim は直下の実体として 1 回だけ数える）
- **`epic/<name>/` とその `next/` `pending/` `done/`**（固定 2 段）

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

🚨 **対象 0 件のときは「抽出して 0 件だった」と書く**。抽出が壊れて空を返した場合も同じ「0 件」に
見えるので、黙って終わると**点検したことと、抽出が空振りしたことが区別できない**。会話履歴に
issue 番号が 1 つも出ていないなら、その事実も 1 行添える（0 件の根拠になる）。

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
