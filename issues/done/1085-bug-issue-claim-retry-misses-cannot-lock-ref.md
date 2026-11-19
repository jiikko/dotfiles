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

- **再試行の条件を「対象の ref が拒否された」かつ「origin の先端が基点から動いた」の 2 つにする**
  - ① ref が拒否されたか: push を `--porcelain` で打ち、対象の ref の行が `!` (拒否) かで見る。人に向けた文言 (`(non-fast-forward)` 等) の grep はやめる。
    pre-push の hook で落ちたときに ref の行が出ないこと (= ① が偽になること) は未実測で、書く前に測る
  - ② origin が動いたか: worktree を切るとき (`git worktree add --detach "$wt" "origin/$branch"`) の sha を基点として控え、push の後に
    fetch した `refs/remotes/origin/$branch` と比べる
  - 両方が真なら取り直して再試行する。どちらかが偽なら今の「push できなかった (pre-push の検査など)」で止める。
    ② だけで決めると、pre-push の検査で落ちたのと同時に他のセッションが push したとき、検査の失敗を競合と取り違えて再試行する (反証レビューの P2)
  - 前例: my-products の `bin/bump-submodule` の `--push` は「失敗の理由は文言でなく origin の今の先端で分ける」として、基点と `git ls-remote` の先端を比べている。
    ただし bump-submodule は pre-push の gate を持たない push なので、② だけで足りている。issue_claim は gate があるので ① も要る
  - fetch に失敗したときは今の rc 3 (載ったかを確かめられない) の扱いのまま。rc の契約 (0 / 1 / 2 / 3) は変えない
- **上限まで負けて終わるときは、各回の push の出力を全部出す**。今は回ごとに `$work/push.log` を上書きし、上限で終わるときは出力を出さないので、
  検査の失敗が「N 回とも origin が先へ進んでいた」に隠れうる
- 上限 `ISSUE_CLAIM_ATTEMPTS` (指定しないと 3) は変えない。ただし再試行のたびに gate (obaket では 2〜3 分) が走り直すので、
  remote が数分おきに進む間は 3 回とも負けうる。そのときの文言は今の「N 回とも origin が先へ進んでいて push できなかった」のままでよい
  (並行のセッションに push を控えてもらう判断は人がする)

## テスト

- `tests/issues/test_issue_claim.sh` の non-fast-forward のケースは `ISSUE_CLAIM_TEST_BEFORE_PUSH` で push の前に origin を進めている。
  `cannot lock ref` は「push が始まった後 (pre-push の hook の中) に origin が進む」ときに出るので、fixture の repo の pre-push hook の中で
  別の clone から origin へ push して作る。この方法で `cannot lock ref` が出るかは未実測で、書く前に 1 回測る
- 変異: 判定を元の文言の grep に戻すと、新しいケースが赤くなることを確かめる
- pre-push の hook を失敗させ、同時に origin も進める複合のケースを足す (今の hook の失敗のケースは origin を動かさない)。
  ② だけで判定する変異でこのケースが赤くなることを確かめる

## 反証レビュー (codex gpt-6.1-sol high、2026-11-13。対象は最初に書いた対応方針)

P1 なし。P2 1 件を採り、P3 1 件は採らずに記録した。中心の説明 (pre-push の間に remote が進むと `cannot lock ref … expected` になる・
今のコードがこの文言を再試行しない) は反証されなかった (git の transport.c / send-pack.c / files-backend.c で裏づけ)。

- P2 (採用): 「origin が動いたか」だけで分けると、pre-push の検査の失敗と同時の他人の push を競合と取り違えて再試行し、
  上限で終わると検査の失敗が隠れる → 対応方針を「ref が拒否された」かつ「origin が動いた」に直し、各回の出力を出す項を足した
- P3 (却下): origin が A → B → A と巻き戻ると、基点 A との比較では「動いていない」になり再試行しない。
  却下の理由: 巻き戻しは既定のブランチへの force-push でしか起きず、この道具が push する既定のブランチではその運用をしていない。
  再提起するなら、既定のブランチへの force-push が実際に起きた記録と、そのとき claim が rc 1 で止まった出力を添えること

## 同じ仕組みの検索 (2026-11-13)

- `grep -rln -e "non-fast-forward" -e "fetch first"` を `~/dotfiles/scripts`・`~/dotfiles/bin`・`~/src/my-products/bin` に当てて、文言で再試行を決めているのは
  `scripts/issue_claim.sh` だけだった。`bump-submodule` は上のとおり先端の比較で分けている
- 文言を grep しない手組みの再試行 (rule の手順・skill の中の push のループ) はこの検索では拾えない

## 関連

- obaket issue 1487 (retro。この issue の出典)
- 764 (`issue_claim.sh` を作った issue)

## 進捗 (2026-11-20)

- [x] 未実測だった 2 点を測った (git 2.55.0、macOS。bare の origin と 2 つの clone で再現)
  - pre-push の hook の中で別の clone から origin へ push すると、`--porcelain` の stdout は
    `!	HEAD:refs/heads/master	[remote rejected] (incorrect old value provided)`、stderr は `remote: error: cannot lock ref … but expected …` になる
  - pre-push の hook が失敗すると、stdout に ref の行は出ない (= ① は偽)。fetch first のときは `!` の行が出る
- [x] 再試行の条件を「対象の ref が拒否された (porcelain の `!` 行)」かつ「origin の先端が worktree を切った時点の基点から動いた」にした。
  上限で終わるときは各回の push の出力を全部出す。再試行の行に拒否の理由を出す
  (commit「fix(issue_claim): pre-push の間に origin が進んだ cannot lock ref も再試行する (issue 1085)」と
  「fix(issue_claim): 拒否の ref の照合を文字列の比較にし、再試行の行に拒否の理由を出す (issue 1085)」)
- [x] テスト: 7.12 (pre-push の中で origin を進める cannot lock ref。前提の `incorrect old value provided` も assert する) /
  7.13 (pre-push の失敗と origin の前進が同時) / 上限で各回の出力が出ること
  - 🚨 fixture の pre-push hook は一時 worktree の `GIT_DIR` を継承している。外さずに `git -C other push` を打つと claim の repo を push し、hook が再帰して fork が尽きた
- [x] 変異 (mutate-verify): 判定を元の文言の grep に戻す → 7.12 だけが red / ref の拒否の条件を外し origin が動いたかだけにする → 7.13 だけが red
- [x] 敵対レビュー (opus) 2 周。1 周目で採ったもの: ブランチ名の `+` `(` で awk の正規表現の照合が外れる → 末尾の文字列の比較に直した /
  7.12 が前提 (cannot lock ref の経路を通ったこと) を確かめていない → assert を足した。2 周目 (直した差分) は再現できる誤りなし

## 敵対レビューで記録だけにしたもの (未対応)

- サーバ側の拒否 (pre-receive・保護ブランチの `[remote rejected] (… hook declined)`) と他人の push が同じ回に重なると、競合と読んで再試行する。
  上限で終わると「N 回とも origin が先へ進んでいて」と出るが、各回の push の出力を出すので原因は見える。旧実装は再試行しなかった。
  直さない理由: 拒否の理由の文言で分けると、この issue で外した文言の判定に戻る。dotfiles・obaket の既定のブランチに pre-receive の拒否は無い。
  再提起するなら、サーバ側の拒否で上限まで回った実際の出力を添えること
- (未実測) push の拒否の直後の fetch が、読み出しの複製の遅れで古い先端を返す remote では、競合でも「origin が動いていない」と読んで rc 1 で止まる。
  旧実装は文言で再試行していた。GitHub で起きるかは測っていない。起きたら、push の出力 (fetch first の行) と fetch の後の先端を添えて再提起する

## 残タスク

- なし (スコープ外のぼやき: 一時 worktree を短い名前 `origin/$branch` から切っているので、同名のローカルブランチ `origin/master` があるとそちらが優先される。未実測)
