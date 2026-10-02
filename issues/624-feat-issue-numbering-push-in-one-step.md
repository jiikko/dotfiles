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
- **HEAD・index・作業ツリーに触らない**。worktree はセッションの開始時に切るので、呼ぶ時点では base が古く、未コミットの変更や未 push の commit もある。
  そこで commit して push する形は、non-fast-forward で弾かれ続けるか (古い base の上で作り直すだけ)、rebase が dirty で止まるか、
  「未 push の commit があれば止める」に毎回かかって使えない (stash は禁止)。代わりに:
  1. `git fetch` → origin/master の tree を読む (`git read-tree` を一時の `GIT_INDEX_FILE` に) → 番号を数える
  2. skeleton の blob (`git hash-object -w`) をその一時 index に足して `git write-tree` → `git commit-tree -p origin/master`
  3. `git push origin <sha>:refs/heads/master`。HEAD は動かさない (呼んだ worktree は、後でいつもどおり rebase して skeleton を取り込む)
  - この形だと、自分の他の commit を巻き込む心配も構造的に無い
- **弾かれた理由を分ける**: non-fast-forward なら 1 からやり直す (回数に上限を置き、超えたら失敗)。それ以外 (pre-push の検査・認証・ネットワーク) なら
  再試行せずに失敗させる。commit-tree の形なら、弾かれた commit は手元のどの ref にも残らない (番号を誰にも見えないまま取った状態が残らない)
  - pre-push は issues/ を触る push を「今回の push が壊したものでなくても」止める (`githooks/pre-push` 冒頭)。master が別の理由で赤いと通らないので、そのときの案内を出す
- 共有の working tree (`~/dotfiles`) では動かさない (worktree の中だけ。`.claude/rules/worktree-per-session.md`)
- 入口の文書: `issues/README.md` の「採番」節をこの script に向ける (`new-tool-requires-entrypoint-docs.md`)
- PG (push しない書き手) は今の `new-*.md` の運用のまま。PG が呼んでも push しない (止める) 形にするかを決める

## 確かめること

- [ ] 2 つの worktree から同時に呼んで、番号がぶつからない (片方が取り直す) ことを確かめた
- [ ] base が古い / 作業ツリーが dirty / 未 push の commit がある worktree から呼んで、1 回で通り、HEAD・index・作業ツリーが変わらないことを確かめた
- [ ] pre-push に止められたとき、再試行せずに失敗し、手元にどの ref も残らないことを確かめた
- [ ] pre-push の検査 (`githooks/pre-push`) が skeleton を通すこと (例: ファイル名に `human` を含む skeleton は `期限:` が要る)

## 進捗

- 2026-10-02: 起票 (epic 618 の子として)。同日、反証レビューを受けて epic から外し、script を第一案に書き直した
- 2026-10-02: 敵対的レビュー (opus) で「worktree の HEAD の上で commit して push する形は、古い base・dirty・未 push の commit のどれでも通らない」
  「pre-push に止められると番号が手元にだけ残る」と壊され、HEAD に触らない commit-tree の形と、弾かれた理由の区別に直した。
  壊せなかった攻め口: 2 つの worktree からの同時実行 (`test_issue_numbers_unique` が pre-push で止める)、epic 配下の数え漏れ、`issue_number_drafts.sh` の stage との混入
