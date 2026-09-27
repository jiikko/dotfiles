# 553 (ux): 「master に無い commit」「記録に無いカード」で残る worktree を、人が判断する入口が無い

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

dogfooding (2026-09-27) で分かったこと。550 の予定で worktree clean が毎日回るようになったが、次の worktree は判定が変わらない限り
永久に残る。何が残っているかは `pro-con worktree clean` の一覧を打たないと見えず、打っても「どうすればよいか」は出ない。

- master に無い commit がある: 3 個 (C-004 / C-009 / C-020。完了のカードで、取り込まれなかった作業)
- 記録に無いカードの worktree: 5 個 (pc-c-026 / 043 / 051 / 057 / 087。削除したカード)

(数は 2026-09-27 13:39 に `pro-con worktree clean` の一覧だけのモードで数えた値)

削除したカードの worktree が永久に残る理由: `card delete` (backend の `DeleteCard`) は worktree とブランチを消さず、片付けの印 (`store.Purged`。
497) も書かない。印を書くのは 1 週間の自動の削除 (`d.purge`) だけなので、削除したカードの worktree は `wtclean` の `cardOf` で
「記録に無いカード」になり、残す側から出られない

## 対応方針 (案)

- 設定画面のディスクのタブ (か、スケジューラージョブのタブ) に「残した worktree と理由」を出し、人が選んで消せる口を置く案
  (消す操作は `pro-con worktree clean` と同じ判定の 1 か所を通す。master に無い commit は `refs/pro-con/removed/` に退避してから)
- 人の番のカードとして上げる案 (「C-004 の取り込まれなかった commit 1 本をどうするか」)
- どちらにしても、消す判断は人が持つ (447 の「取り込む前の作業が入っているかもしれない」は変わらない)

## 受け入れ条件

- [x] close: PG のブランチ (worktree の先端・`worktree-pc-<カード>`) に origin/master に無い commit があれば、dispatcher が完了を除けて理由を出す。
      取り込まない終わり方 (`--ending answered` / `investigated` / `rejected`) を付けたときだけ通す。判定は wtclean の inBase と同じ 1 か所
- [x] 取り込まない終わり方で閉じたカードは、PG を止め終えた直後に commit を `refs/pro-con/removed/pc-<カード>/<sha>` に残してから worktree とブランチを消す
- [x] delete: 同じく、記録から外した直後に commit を残してから worktree とブランチを消す。未 commit の変更があるものは消さずに残す (下の決定)
- [x] 受け皿: それでも残った worktree の一覧と理由が、コマンドを打たなくても設定画面のディスクのタブで見える
- [x] 受け皿: 人がその場で「消す」(commit を残してから)「残す」を決められる
- [x] 消す操作はどれも消す直前に材料を取り直して判定し直し、テストは置き場の外を実行前に拒否する (sandbox-real-destructive-test-apis / adversarial-review-own-safeguards)

## 進捗 (2026-09-27、pro-con カード C-005)

- commit「pro-con: 閉じる・消すその場で PG の worktree を片付け、残ったものはディスクのタブで人が決める (553)」
  - close の検査: `dispatcher/worktree.go` の `checkClose` (store の `ApplyChecked` の検査の口から。判定は `wtclean.Unlanded` = 片付けと同じ `inBase`)。
    止め終えた後にも見直す (`recheckClosed`。codex の敵対的レビューの指摘: 検査の後・止める前に PG が commit できる)
  - 取り込まない終わり方・削除の片付け: `wtclean.Settle` (取り直して判定 → 取り込み済みは Clean と同じ、取り込み先に無い commit・記録に無いカードは
    `Discard` = 先端と reflog を `refs/pro-con/removed/` に残してから消す)。dispatcher は `worktree-clean.lock` を取ってから呼ぶ
  - 受け皿: `ui/settingswt.go` (ディスクのタブの「残した worktree」。D を 2 回で消す・L で残す = `git worktree lock` の理由 `pro-con: 人が残すと決めた`)
- 確かめたこと (単体テスト): `wtclean/decide_test.go` (本物の git を sandbox の中で。Ask の判定・Discard・見た後に変わったものを消さない・Hold・Unlanded・Settle)、
  `dispatcher/worktree_test.go` (close を除ける・確かめられないときも除ける・取り込まない終わり方で片付け・削除で片付け・止め終えた後の見直し)、
  `ui/settingswt_test.go` (一覧と理由・D 2 回 / L・見ているだけの画面は決めない)。新しいテストは mutation (11 本) で全部落ちるのを確かめた
- `make -C src/pro-con test` (go test -race ./...) は全パッケージ ok。lint は `CGO_ENABLED=0 make -C src/pro-con lint` で 0 件
  (cgo ありだと golangci-lint のビルドが clang と macOS 27 SDK の版ずれで落ちる = この変更の前の段階。errorlint 1 件は commit「close の検査のエラーを %w で包む」で直した)
- 残り: 実物の dispatcher と repo での確かめはしていない (今ある残り物 C-004 / C-009 / C-020 / pc-c-026 等をディスクのタブで消すのは人が行う)。
  削除したカードの session・transcript は片付けない (wtclean の session の片付けは「記録に無いカードの session」を残す。前からの挙動)

## 関連

- 492 / 550 / 447 / 456 (設定画面のディスクのタブ)

## 決定

### 2026-09-27 (方針変更。pro-con カード C-005 の人の追加オーダー)

- **主の経路は「閉じる・消すその場で決着をつけて、残り物を作らない」**。後から人が判断する入口は主にしない
  - 背景: C-004 / C-009 / C-020 の「master に無い commit」は 9/25、取り込みの係 (487) ができる前に人・PM が手で取り込んだ時期の残り物。
    今の取り込みの係は git merge で入れるので、正常な流れでは新しく起きない。今の流れでも残る経路は (a) card close が PG のブランチが
    master に入っているかを確かめない (人に回されたカードを人が画面で閉じると PG の commit が宙に浮く) と (b) card delete が worktree と
    ブランチを残し、片付けの印も書かない (「記録に無いカード」) の 2 つ
- close は取り込み済みかを確かめ、入っていなければ除ける。取り込まずに閉じると明示したとき (取り込まない終わり方) だけ通し、その場で退避してから消す
  - 取り込まない終わり方 = `answered` / `investigated` / `rejected` (`card.Ending.Discards`)。`pending-issue` は後で取り込むかもしれないので含めない
- delete も、記録から外した直後に退避してから消す
- **未 commit の変更は退避しない (残す)**: PG を止めた後の未 commit の変更は、PG が review の前に commit し忘れたか、止める途中の書きかけ。
  自動で commit に固めて `git worktree remove --force` すると、固めた後・消す前に書かれたファイルを失う窓ができる。残して受け皿に出し、
  人が中を見て commit してから「消す」を決める (commit すれば受け皿で決められる行になる)
- 設定画面のディスクのタブは**最悪のときの受け皿**としてだけ残す (18:35 の回答「ディスクのタブを入口にする」はこの意味に狭める)。
  前からの残り物・退避や削除に失敗したものを理由つきで出し、人が選んで「消す (退避してから)」「残す」を決める。最小限でよい
- 並行するカードとの衝突の見積もり (前の回答から変わらない): C-002 (556) / C-003 (555) / C-004 (554) とは触る場所も変える判断も重ならない

### 2026-09-27 18:35 (前の回答。上の方針変更で受け皿に狭めた)

- 入口は設定画面のディスクのタブに置く: 残した worktree の一覧と理由を出し、人が選んで「消す」「残す」を決める。人の番のカードとして上げる案は採らない
- 消す操作は `pro-con worktree clean` と同じ判定の 1 か所を通し、master に無い commit は `refs/pro-con/removed/` に退避してから消す
