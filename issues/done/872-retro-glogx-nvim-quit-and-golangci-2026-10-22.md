# 872 (retro): glogx の nvim の Ctrl-G・golangci-lint の linter 追加と版上げの振り返り

起票日: 2026-10-22

## 概要

1 セッションで 3 本を push した: glogx が開いた nvim の中の Ctrl-G で glogx ごと閉じる (a73854c0)、
全 23 モジュールの golangci-lint にバグ検出系の linter と govet の全 analyzer を足す (ba03b908)、
golangci-lint を v2.5.0 → v2.15.0 に上げる (0fa4cdaa)。踏んだところだけを書く。

## 踏んだところ

### 1. rebase で他人の変更を取り込んだ後、検査を回し直さずに push した (a73854c0)

non-fast-forward で弾かれた push を「fetch → rebase → push」で繰り返したとき、再試行の中で取り込んだ別セッションの
変更 (f653dea9 など) が glogx の `tui.go` と tuikit に及んでいたのに、検査を回さずに push した。push の後に glogx / tuikit の
テストと lint を回して緑を確かめたので実害は無かった。規範は既にある (`verify-execution-not-just-exit-code.md`
「push の前に他人の commit を rebase / merge で取り込んだら、それも編集に数えて push する木で回し直す」)。
破ったのは、再試行を手で 1 回ずつ打っていて、検査の段が手順に無かったため。後の 2 本 (ba03b908 / 0fa4cdaa) は
「rebase で入った差分が触るモジュールを数え、そのモジュールの lint とテストが緑のときだけ push する」ループに
書いて、同じ形を踏まなかった。

- 提案: push の再試行を書くときは、rebase で入った差分のうち自分の検査の範囲に触れるものを数え、
  その検査を回す段を**再試行の中に**置く (手で打つ再試行は「もう一度 push するだけ」に縮みやすい)。
  切り出し先: `_claude/rules/verify-execution-not-just-exit-code.md` の上の行への追記

### 2. 保存の成否を「例外が出たか」で判定し、確認を断られた経路を見落とした (P1)

nvim の `<C-g>` を `pcall(vim.cmd, "wall")` が成功したら `:cquit` する形で書いた。外から書き換えられた
ファイルの上書き確認 (W12 の後の y/n) で n を答えると、`:wall` は例外を投げずに変更を残したまま戻り、
`:cquit` が編集を捨てた。敵対的レビュー (opus) が実測で再現し、「書けるはずのバッファに変更が残っていないか」
で判定する形に直した (テスト 2b)。自分の正常系の試行では、確認が出る経路を通らなかった。

- 提案: 失敗を例外・rc で判定する書き込み・保存は、確認で断られる・一部だけ書ける経路でも例外 / rc が出るかを
  確かめ、出ないなら成否を**結果の状態**で判定する。切り出し先: `_claude/rules/verify-execution-not-just-exit-code.md`
  の「確認を求める外部コマンドは、非対話だと何もせず rc=0 で返る」節への追記 (同じ形の対話版)

### 3. 効かない修正を、効くと書いたコメント付きで入れた

1 周目の敵対的レビューの P3 (「取り直しが終了の後始末の後に走る」) に対して、`ReloadAfterEdit()` を
`handleQuitKey` より先に呼ぶ並べ替えと「後にすると cancelAll の後で走査を起こし直す」というコメントを入れた。
`ReloadAfterEdit` は `tea.Cmd` (予約) を返すだけで、走査はどちらの順でも Update の後に走るので、効果は無かった。
2 周目のレビューが指摘し、revert した。

- 却下: 1 例だけで、既存の網 (敵対的レビューの 2 周目・「効果がなかった修正は revert する」) が捕まえた

### 4. make test の赤が 3 回とも、並列負荷で時間の上限に当たったテストだった

- `tests/claude/test_deny_bare_tmux_kill.sh` (90KB 入力) と `test_deny_piped_push_then_destroy.sh`: hook が 10 秒の timeout に達した
- `_claude/mods/tool-elapsed` の plugin test (`tests/claude/test_claude_mods.sh` が mod ごとに回す。5 分ごとの toast のテストが 10 秒を超えた)
- CI の `src/restartable` の `TestPTYRunnerDrainsLateOutputDuringTerminationWithInputBurst` (leader PID が空で読まれた。同じ commit の再実行で success、手元で 20 回連続 pass)

どれも単独では通り、私の変更 (Go と lint の設定) と経路がつながらないことを 1 本ずつ確かめるのに時間を使った。
帰属の確認そのものは要るが、同じ日に別々のテストで 3 回起きている。

- 提案: 並列負荷でだけ落ちるテストを、どのテストが何回落ちたかで集計する入口を作り、帰属の確認を毎回
  一からやり直さない。切り出し先: 新規 issue (869 の serial-run は重い検査の同時実行を減らす側で、
  こちらは「負荷で落ちたテストの記録」側なので別にする)

### 5. 本体への pull が一度だけ rc=128 で止まった

`scripts/pull_main_checkout.sh` が `fatal: Cannot rebase onto multiple branches` で止まった (ba03b908 の push の直後)。
直後に見ると作業ツリーはクリーンで、他のセッションの pull で私の commit まで取り込まれていた。原因は調べていない。
並行した fetch が FETCH_HEAD を書いている途中を読んだ形を推測しているが、未確認。

- 却下 (局所): 1 回だけで再現手段が無い。再発したら、同じ時刻に走っていた fetch / pull を `ps` で見る

## 残課題

- [x] 1 の追記 (`verify-execution-not-just-exit-code.md`) — ユーザーの指示 (2026-10-22) で追記した。「最後の編集の後に gate を回す」の下に、
  non-fast-forward の push の再試行の中に検査の段を置く項目を足した (同じ日に dotfiles-b9 も 861 の push で同じ形を踏んだ)
- [x] 2 の追記 (`verify-execution-not-just-exit-code.md`) — 同じく追記した。「確認を求める外部コマンドは…rc=0 で返る」の節に、対話の中の
  書き込み・保存を例外 / rc で判定するときの項目を足した
- [x] 4 の新規 issue — 873 に起票した (反証レビュー済み。負荷でだけ落ちるテストの issue が 422・864 の A9・871 に散っていることも書いた)
- [x] `.golangci.yml` の 23 個の写し — 874 に起票した (反証レビュー済み。既存の検査 3 本が共通の塊の一部だけを見ていることを書いた)
