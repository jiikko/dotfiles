# 808 (bug): 長いセッションで、issue-progress-check が取り込んだ他セッションの commit の issue を何度も挙げる (353 の再評価の trigger)

起票日: 2026-10-16
優先度: P3 (誤報で止まるだけ。ただし本物の更新漏れの指摘が誤報に埋もれる)

反証レビュー: 自己レビューのみ (観測の記録と、353 の判断を見直す材料の整理。下の件数は obaket のセッションの経過から数えた)。

## 概要

issue 353 (done) は、`_claude/hooks/issue-progress-check.sh` が他セッションの commit に付いた issue を「このセッションが関わった issue」に数える誤報を、
「dedup で 1 セッション 1 行」として受け入れ、直さないと決めた。再評価の trigger は「1 行では済まない形 (毎セッション別の issue で出続ける /
実際の更新漏れが誤報に埋もれる)」。2026-10-16 の obaket の 1 セッションで、その形を観測した (起点: obaket issue 1166 の retro)。

## 観測 (2026-10-16、obaket の 1 セッション)

- セッションは長く (朝から深夜まで)、worktree で作業して push し、共有 checkout を `pull --rebase` で追いつかせる、を 10 回以上繰り返した
  (obaket の `.claude/rules/worktree-per-session.md` の標準手順)
- turn の終わりの block は**少なくとも 5 回**。挙がった issue は 1128 / 1143 / 1145 / 1114 / 1107 / 1080 / 1088 / 978 / 1092 (と、476 を参照する
  open issue)。**どれもこのセッションでは触っていない** (rebase / pull で取り込んだ他セッションの commit の subject・変更パスから入った)
- 毎回「他のセッションの commit による変更なので更新不要」と返して閉じた。同じ issue は 2 度出ないが、取り込むたびに新しい他人の issue が入るので、
  長いセッションほど回数が増える

## 対応方針 (案。どれも未検証)

- 353 の「次に試すなら」(開始時の `worktree list` や時刻を `issue-progress-start.sh` が控えて材料にする) を検討の起点にする
- Stop の hook JSON に `transcript_path` があるなら (未確認)、「このセッションの transcript に、その issue のパス・番号への書き込み (Write / Edit / Bash の
  commit) の痕跡があるか」で絞る案。commit の祖先関係や reflog と違って、worktree を消しても残る。353 の revert の原因 (標準手順で恒久的に無音になる) に
  当たらないかを A-B で測ってから採る
- 直さないままにするなら、353 の trigger を「長いセッションでは数回出る」前提で書き直す

## 関連

- dotfiles issue 353 (done。誤報の原因の実測と、直さない決定)
- obaket issue 1166 (retro。この観測の起点)

## 進捗

- [ ] 353 の判断を見直す (直すか、trigger を書き直すか)
