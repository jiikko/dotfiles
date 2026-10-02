# 624 (feat): issue の採番を、push まで 1 コマンドで済ませる入口を作る

起票日: 2026-10-02

> 起票時は epic 618 (Claude Code の mods) の子で、mod のツール (`$.tool.register`) として計画していた。
> 反証レビューで「script 1 本で同じことができ、mod にする理由が無い (mod の失敗モードだけが増える)」と指摘され、epic から外した。

## 概要

issue 番号の衝突が繰り返している (`.claude/rules/worktree-per-session.md` に 214 / 221 / 223 / 295 の 4 例)。
規約は「採番したら即 push」だが、本文を書いてから commit するあいだに他のセッションが同じ番号を取る。
番号の一意性は `tests/issues/test_issue_numbers_unique.sh` と `githooks/pre-push` が**事後に**検出するだけ。

「fetch → 次の番号を数える → skeleton を置く → その 1 ファイルだけを commit → push」を 1 コマンドにすれば、
番号が push されるまでの窓が、本文を書く時間からコマンド 1 回分に縮む。

## 既にある判断との関係

`scripts/issue_number_drafts.sh` (issue 530) の冒頭は「**番号を払い出す口を別に作ると、pro-con の外で採番する人・session からその予約が見えない**」として、
予約の仕組みを作らなかった。この入口は予約ではなく、**master へ push された skeleton そのものが番号の取得**になる
(他の人・session からは、今の規約どおり origin/master の `issues/` に見える)。530 の判断とはぶつからない見込み。実装時に 530 の実装と一緒に見直す。

## 対応方針

- `scripts/issue_take.sh <置き場> <type> <slug> <タイトル>` (名前は仮。置き場は `issues/` か `issues/epic/<name>/`)
- 数え方は `issues/README.md` の採番手順と同じ母集合 (working tree と origin/master の `issues/` 全体)。
  採番の式は 1 箇所に寄せ、README の手順・`issue_number_drafts.sh`・この script から共有する
- commit は pathspec で skeleton の 1 ファイルだけ (`commit-with-pathspec.md`)。push の前に `git log origin/master..HEAD` が
  その commit 1 本だけであることを確かめ、他の未 push の commit があれば push せずに止める (他の commit を巻き込まない)
- push が non-fast-forward で弾かれたら、fetch して番号を取り直し、skeleton を作り直す。push の rc は直接取る (`| tail` に通さない)
- 共有の working tree (`~/dotfiles`) では動かさない (worktree の中だけ。`.claude/rules/worktree-per-session.md`)
- 入口の文書: `issues/README.md` の「採番」節をこの script に向ける (`new-tool-requires-entrypoint-docs.md`)
- PG (push しない書き手) は今の `new-*.md` の運用のまま。PG が呼んでも push しない (止める) 形にするかを決める

## 確かめること

- [ ] 2 つの worktree から同時に呼んで、番号がぶつからない (片方が取り直す) ことを確かめた
- [ ] 他の未 push の commit があるときに止まることを確かめた
- [ ] pre-push の検査 (`githooks/pre-push`) が skeleton を通すこと (例: ファイル名に `human` を含む skeleton は `期限:` が要る)

## 進捗

- 2026-10-02: 起票 (epic 618 の子として)。同日、反証レビューを受けて epic から外し、script を第一案に書き直した
