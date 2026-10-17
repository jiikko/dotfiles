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

- [x] 353 の判断を見直す → 直す (2026-10-18、ユーザーが「やって」)。material を git から transcript に変えた
- [x] 実装: `primary` (commit subject の `(NNN)` と next/ の claim) を、このセッションの transcript の assistant の tool_use の入力に
  番号が出たものだけに絞る。transcript を読めない・jq が無い・tool_use が 0 件なら「測れない」として絞らない
- [x] テスト (`tests/claude/test_issue_progress_check.sh` の「issue 808」の節) と変異 4 本 (mutate-verify で全部 red):
  M1 絞り込みを外す / M2 0 件を測れないに戻す段を外す / M3 行全体を数える (地の文の番号を拾う) / M4 `fromjson?` を `fromjson` にする
- [x] 本物のデータの A-B (2026-10-18、このセッションの transcript を最初の Stop の直前の 152 行で切り、基準点 9bf58a45):
  旧版は 9 行 (495 / 497 / 551 / 439 / 814 / 369 / 774 / 808 / 818。どれもこのセッションでは触っていない)、新版は無出力
- [x] 敵対的レビュー (opus) の結果を反映した。動作は壊せなかったが、長いセッションでは絞り込みが半分ほどしか効かない (P2) との指摘:
  - P2 採用: 「番号が数字として出たか」で照合していたため、行番号 (`sed -n 1092,1199p`)・sha (`c394f5d1`)・scratchpad の `claude-501`・
    別 issue の slug の中の番号 (`1114-verify-1107-host`) が当たっていた。照合を primary の出所と同じ形 (issue のパス `/NNN-` /
    commit message の `(NNN)` / `issue NNN` と `issue_claim.sh NNN` の類) に限った (hook の `touched_nums`)
  - P3 性能 採用: 番号ごとの grep (41 個で 3.4 秒) をやめ、1 回の抽出と `comm` にした
  - P3 テストの穴 採用: 部分一致・行番号・sha・`claude-NNN`・slug の中の番号を fixture に足した
  - 記録のみ (どれも今の挙動より悪くならない): サブエージェントの transcript (`<session>/subagents/*.jsonl`) は読まない /
    cherry-pick・revert の commit は入力に番号が出ない / fork は親の履歴の番号も数える / tool_use が 0 件のセッションは絞らない
- [x] 変異 8 本 (mutate-verify で全部 red): 上の M1〜M4 に加え、M5 照合を数字の抽出に戻す / M6 パスの形を外す /
  M7 `(NNN)` の形を外す / M8 `issue NNN` の形を外す
- [x] 再生: 808 の観測元の obaket のセッション (bc1c39bd) の tool_use の入力で、「触っていない」9 個 (1128 / 1143 / 1145 / 1114 / 1107 /
  1080 / 1088 / 978 / 1092) を旧式 (数字の境界) と新式で数えた (`/usr/bin/grep`)。旧式は 9 個全部、新式は 5 個が当たり、
  残りはどれも旧版の hook の指摘に反応して打った `git log -- issues/1128-…` の類 (新版ではそもそも指摘されないので起きない)
- [x] A-B (このセッション): 最初の Stop の時点で旧版 9 行 → 新版 無出力 (0.19 秒)。全体の transcript では、実際に編集した 747 の
  参照元 3 行と 818 (別 repo の同じ番号) だけが残る (0.48 秒)

## 分かっている限界 (2026-10-18)

- 過剰の向きが残る: 他人の issue のパスや `(NNN)` を自分のコマンドに書くと (hook の指摘を確かめる `git log -- issues/NNN-…` 等)、その番号は「触った」に入る。同じ行の再掲は dedup が止める
- 過少の向きが増える: サブエージェントだけが触り、親の tool_use の入力 (Agent の prompt 等) に番号が出ていない issue は指摘されない
- 関連 issue の列挙は番号の単語一致のままなので、別 repo の同じ番号 (818 が指す screen-share-app の 808) も挙がる。今回は対象外
