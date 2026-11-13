# 1085 (bug): issue_claim.sh が push の「cannot lock ref」を再試行せずに失敗で終わる

起票日: 2026-11-13

## 概要

pre-push の gate を走らせている間に origin が進むと、push は non-fast-forward ではなく `cannot lock ref … expected` で弾かれる。
`scripts/issue_claim.sh` はこの形を再試行の対象にしておらず、「push できなかった」と出して rc 1 で終わる。
出典は obaket の retro 1487 の 1 (2026-11-12)。

## 観測された事実 (obaket retro 1487 から引用)

- `issue_claim.sh 1455` の push が 3 回とも `! [remote rejected] HEAD -> master (cannot lock ref 'refs/heads/master': is at X but expected Y)` で失敗した
- obaket の pre-push の gate (`make lint-gates`) は 2〜3 分かかる。その間に別のセッションが obaket の issue の docs を push していた
- 並行のセッションに 10 分 push を控えてもらってから打ち直すと、1 回で通った

## 原因 (2026-11-13 に `scripts/issue_claim.sh` を読んで確認)

- 再試行するかは、push の出力の文言だけで決めている: `grep -q -e '(non-fast-forward)' -e '(fetch first)' "$work/push.log"`
  (push の後の分岐。コメント「non-fast-forward の判定は git の文言だけで見る」の行)
- git は push の前に remote の先端を読み、その値を期待値にして ref の更新を頼む。pre-push の hook が走っている間に remote が進むと、
  更新の段で期待値と食い違って `cannot lock ref … expected` になる。文言に `(non-fast-forward)` も `(fetch first)` も含まれないので、
  再試行の枝に入らず、`die "push できなかった (pre-push の検査など…)" 1` に落ちる
- push の後に `fetch_branch` で origin の対象のブランチを取り直し、claim の commit が載ったかは既に確かめている。
  再試行の判断に要る「origin の先端が基点から動いたか」は、この fetch の結果で分かる

## 対応方針

- **再試行の分類を、文言ではなく「origin の先端が、worktree を切った基点から動いたか」で決める**。
  worktree を切るとき (`git worktree add --detach "$wt" "origin/$branch"`) の sha を基点として控え、push の失敗後に fetch した
  `refs/remotes/origin/$branch` と比べる。動いていれば (non-fast-forward・fetch first・cannot lock ref のどれでも) 取り直して再試行し、
  動いていなければ今の「push できなかった (pre-push の検査など)」で止める
  - 前例: my-products の `bin/bump-submodule` の `--push` は「失敗の理由は文言でなく origin の今の先端で分ける (文言はバージョン・ロケールで変わる)」として
    `git ls-remote` の先端と基点を比べている。同じ形に揃える
  - fetch に失敗したときは今の rc 3 (載ったかを確かめられない) の扱いのまま
- 文言の判定 (`(non-fast-forward)` の grep) は消す。残すと、2 つの判定が食い違ったときにどちらが正かが分からない
- 上限 `ISSUE_CLAIM_ATTEMPTS` (指定しないと 3) は変えない。ただし再試行のたびに gate (obaket では 2〜3 分) が走り直すので、
  remote が数分おきに進む間は 3 回とも負けうる。そのときの文言は今の「N 回とも origin が先へ進んでいて push できなかった」のままでよい
  (並行のセッションに push を控えてもらう判断は人がする)

## テスト

- `tests/issues/test_issue_claim.sh` の non-fast-forward のケースは `ISSUE_CLAIM_TEST_BEFORE_PUSH` で push の前に origin を進めている。
  `cannot lock ref` は「push が始まった後 (pre-push の hook の中) に origin が進む」ときに出るので、fixture の repo の pre-push hook の中で
  別の clone から origin へ push して作る。この方法で `cannot lock ref` が出るかは未実測で、書く前に 1 回測る
- 変異: 判定を元の文言の grep に戻すと、新しいケースが赤くなることを確かめる

## 同じ仕組みの検索 (2026-11-13)

- `grep -rln -e "non-fast-forward" -e "fetch first"` を `~/dotfiles/scripts`・`~/dotfiles/bin`・`~/src/my-products/bin` に当てて、文言で再試行を決めているのは
  `scripts/issue_claim.sh` だけだった。`bump-submodule` は上のとおり先端の比較で分けている
- 文言を grep しない手組みの再試行 (rule の手順・skill の中の push のループ) はこの検索では拾えない

## 関連

- obaket issue 1487 (retro。この issue の出典)
- 764 (`issue_claim.sh` を作った issue)
