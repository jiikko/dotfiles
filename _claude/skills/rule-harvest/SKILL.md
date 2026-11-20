---
name: rule-harvest
version: 1.0.0
description: このセッションで増減した実装・仕組み (コード・検査・hook・設定) から、今後の改修でデグレさせないための恒久的な決定事項を探し、既存の rule と比べて重複・衝突を除き、5 つの観点の反証レビューを通った候補だけを rule として足す (既存の rule への追記を先に考える)。Stop hook rule-harvest-check が促したとき、「この作業から rule に落とせることは無い?」「決めたことを rule にして」「デグレしないための rule を足して」「rule-harvest」で発火。作業の進め方の振り返りは retro (issue)、差分の用語の揺れは refresh-on-session、issue 本文の書き戻しは issue-writeback の担当。
---

# Rule Harvest

実装に埋め込んだ決定 (状態の網羅の仕組み・待ちの契約・ユーザー決定・「なぜこう書かないか」) のうち、コンパイラ・lint・テスト・hook が止めない部分は、
rule に書かないと次にその領域を変える人に届かない。この skill は、作業を終えた時点でそれを拾い、既存の rule と食い違わない形で足す。

- **retro との境界**: retro は「どこで踏んだか」(作業の進め方) を issue に書き、提案の実行はユーザーの判断を待つ。この skill は「実装が何を守っているか」を
  rule にし、反証レビューを通った候補は足す (2026-11-20 ユーザー指示)。既存の規範・ユーザー決定と衝突する候補だけは足さずに聞く
- 候補が 0 件なら、そう書いて終える。レビューは起こさない

## 1. 対象を集める

- Stop hook が並べた commit (`git show --stat <sha>`) と、このセッションの会話 (決めたこと・ユーザー決定・採らなかった案・レビューで採った / 却下した指摘)。
  hook を経ずに頼まれたときは、会話に出た commit と、作業した repo の `git log` (worktree で作業したなら worktree も) から、このセッションの commit を拾う
- 足した仕組みだけでなく、**消した・緩めた仕組み**も見る (外した防御が隠していた失敗は `~/.claude/rules/list-masked-failure-modes-before-removing-guard.md`)

## 2. 候補を挙げる

**差分が数行の修正・機械的な置換・設定値の変更だけなら、ここで「rule 候補なし」と書いて終える** (決定事項を持たない差分にレビューを起こさない)。

候補にするのは、次の 3 つを全部満たすものだけ。

1. **恒久的**: このタスクが終わっても、次にその領域を変える人が知らないと壊す (状態を足す・読み手を足す・待ちを足す・置き場所を変える、のような、未来の変更で読まれる)
2. **機械で止まらない**: コンパイラ・lint・テスト・hook が既に止めるものは書かない (`~/.claude/rules/comment-no-restate-enforced.md`)。
   機械で止められるなら、rule より先に lint・テストを足す案を候補に添える (`~/.claude/CLAUDE.md`「設計方針」の再発防止)
3. **発動点を言える**: 「何をしようとした瞬間に読むべきか」が 1 行で書ける

候補にしないもの: このタスクだけの手順 / 1 か所のコードの「なぜこう書かないか」(そのコードのコメントに書く。`pending-issue-rationale-in-code.md`) /
好み / issue の進捗 / 作業の進め方の振り返り (retro)。

候補ごとに、発動点・規範の文案・それを守らないと起きること (実例があれば commit か issue) を 1 組にする。

## 3. 既存の rule と比べる

候補ごとに、発動点の語 (「状態を足す」の動詞)・規範の語・候補が名指しする型や関数の名前の 3 つで次を grep し、当たった節は**本文まで読む** (見出しで判断しない)。

- 全部の repo に当てはまるもの: `~/.claude/CLAUDE.md`・`~/.claude/rules/*.md`・`~/dotfiles/_claude/rules-checklists/`・`~/dotfiles/_claude/issue-rules.md` と `issue-rules.d/`・関係する skill の SKILL.md
- その repo のもの: root と触ったディレクトリの CLAUDE.md・`.claude/rules/*.md` (paths: 付きも)・rule が正本として指す文書 (docs)・`issues/README.md`
- 同じ判断を記録した issue とユーザー決定 (「ユーザー決定」「ユーザー指示」で grep する。印の無い制約も本文で見る)
- このセッションの retro の提案と、open の retro issue (同じ候補を retro が提案済みなら、そちらの判断を待つ)

判定は 4 つ (5 の反証レビューで落ちたものは、後で「却下」にする)。結果は候補ごとに「比べたファイル → 判定 (根拠の行)」で書き残し、レビューに渡す。
repo の rule と global の rule の両方に当たったら、範囲の狭い repo の rule に追記する (repo をまたいで当てはまるときだけ global)。

| 判定 | 扱い |
|---|---|
| 重複 (同じ規範が既にある) | 足さない。既存の rule が古い・弱いなら、その rule を直す候補に変える |
| 追記 (同じ発動点の rule がある) | その rule の該当項へ追記する。**これを先に考える** (rule は毎セッション読まれ、増えるほど読まれ方が薄まる) |
| 衝突 (既存の規範・ユーザー決定と食い違う) | 足さない。既存を書き換えない。どちらを残すかを最後にユーザーへ聞く |
| 新規 | 発動点が既存のどれとも違うと 1 行で言えるときだけ。その 1 行を書き残す |

## 4. 置き場所を決める

| 当てはまる範囲 | 置き場所 |
|---|---|
| その repo のコードだけ | repo の `.claude/rules/<name>.md`。発動するファイルが決まるなら `paths:` で限る。repo の CLAUDE.md に rule の一覧 (常時読み込みの一覧・paths の一覧) があれば、該当する方へ入口を 1 行足す |
| repo をまたぐ | dotfiles の `_claude/rules/<name>.md` (本文は規範だけ。なぜ・実例は `_claude/rules-rationale/<name>.md`)。特定のファイルでだけ要るなら `paths:` |
| ある手順の途中でだけ要る | その手順の skill・`_claude/rules-checklists/` |

- 常時読み込みの rule を足す・伸ばすなら、**書く前に** dotfiles の `make test-instruction-budget` で余裕を数える (global の層は上限近くで、余裕が数百字しか無いことが多い)。
  足りなければ、新規の rule ではなく既存の rule への追記・`paths:`・checklist を最初から選ぶ
- dotfiles を書くなら dotfiles の `.claude/rules/worktree-per-session.md` に従う (worktree・push・本体への pull)。dotfiles 直下の `CLAUDE.md` の「`_claude/` (Claude Code 設定の正本) を触るとき」も読む

## 5. 5 つの観点で反証レビューする

起こす前に `ratelimit -source claude -check` で 5h 枠を見る。rc=1 (超えている) なら、縮める (観点 2 と 3 だけを残し、1・4・5 は自分で見る) か後に回すかをユーザーに聞く。
rc=3 (判定できない) なら、その旨を書いて 5 体で進める (`~/.claude/rules/subagent-model-tiering.md` の 5h 枠の項)。

候補の文案・置き場所・3 の比較の結果を渡し、read-only のサブエージェントを**観点ごとに 1 体、全部の候補をまとめて 1 回**起こす (候補ごとに起こさない)。
依頼は「この候補は誤りだという前提で壊せ。壊せなければそう書け」にする (「レビューして」と頼むと追認が返る)。

1. **事実**: 文案の名前・パス・挙動・issue 番号・ユーザー決定が、コードと文書に合うか
2. **重複・衝突**: 3 の比較の見落とし (別の名前で同じ規範・正本の文書との食い違い・既存のユーザー決定)
3. **強制済み・機械化**: コンパイラ・lint・テスト・hook が既に止めていないか。機械で止める方が確かではないか
4. **恒久性と置き場所**: このタスク限りではないか。置き場所 (global / repo / paths / skill) と発動点が合うか。常時読み込みの費用に見合うか
5. **使う人の目**: 発動点で読んだ人が、そのまま従えるか (曖昧な語・参照先の不在・長すぎる文)。未来の変更を 1 つ仮定して手順を辿る

- モデルは `~/.claude/rules/subagent-model-tiering.md` に従う (1・5 は sonnet でよい。2〜4 は判断を含むので main モデル)
- 作業者への指示に、読み取りだけ・どこにも書かない・ビルドやテストを回さない、を名指しで書く (読み取りのコマンドは使ってよいと書く)
- 指摘は裏を取ってから採る。再現しない指摘・既存のユーザー決定と食い違う推奨は採らず、理由を残す (`~/.claude/CLAUDE.md`「レビュー方針」)
- 直して規範の中身が変わったら、直した文案に 1・5 の観点でもう 1 周当てる
- 生き残らなかった候補は、却下の理由と一緒に 7 の報告へ回す

## 6. 足す

- 書く: 冒頭に発動点、本文は規範だけ。ユーザー決定は日付つき。「なぜ」は 1〜2 行か rationale へ。止めないもの・強制済みのものは書かない
- 入口: rule の一覧・paths の行・skill の表 (`~/.claude/rules/new-tool-requires-entrypoint-docs.md`)
- 検査: repo の lint (dotfiles なら `make test-lint`・`make test-instruction-budget`)
- commit: pathspec で、rule とその入口だけの commit にする (ほかの作業と混ぜない)。message に候補ごとの判定・比べたファイル・レビューの結果・却下した指摘を書く。
  rule の代わりに lint・テストを足すなら、それは別の commit にする (md 以外を含むので、Stop hook は block の続きの turn でなければ次に数える)
- push: 先にその repo の push の規約を読み、その順で決める
  1. worktree で作業した repo (dotfiles など): 規約どおり push と本体への pull まで行う。聞かない
  2. 共有の checkout で、issue を伴わない push をユーザーの指示で行う repo (my-products の app など): commit で止め、push の可否を聞く
  3. push するとき submodule の中の repo なら、submodule を先に push し、親の参照はその repo の submodule の規約で上げる

## 7. 報告する

候補ごとに 1 行の表で返す。衝突でユーザーに聞くものは、足すものを足した後にまとめて聞く。

| 候補 | 判定 (新規 / 追記 / 重複 / 衝突 / 却下) | 置き場所 | 理由 |
|---|---|---|---|

候補が 0 件なら「rule 候補なし」と、なぜ無いか (機械で止まっている・このタスク限り) を 1 行書く。

Stop hook を無人で回すセッション (無人の `claude -p` など) で止めたくないときは、起動する側で環境変数 `CLAUDE_RULE_HARVEST=off` を渡す。
