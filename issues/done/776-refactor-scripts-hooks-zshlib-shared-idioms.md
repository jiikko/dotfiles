# 776 (refactor): scripts・hooks・zshlib の繰り返しを寄せる (観測ログの既定パスの取りこぼしを含む)

起票日: 2026-10-16

## 概要

ユーザー依頼 (2026-10-16): 「他にも繰り返し呼び出している箇所がないか調べて同じことをやって」。bin (775)・src (773 / 777) の続きで、scripts/・_claude/hooks/・zshlib/ が対象。
read-only のサブエージェント 1 体 (opus) が洗い出し、main が本文と実測で確かめた。

## 母集合の数え方

- scripts/ 直下の .sh 58 本・scripts/lib の 9 本・_claude/hooks の 18 本 + lib 2 本・zshlib の 21 本
- 観点ごとに grep: 数値の検査 (`[!0-9]` / `^[0-9]+` / `<->`)・置き場の解決 (dirname / BASH_SOURCE / DOTFILES_DIR / rev-parse)・tmux への問い合わせ・hook の入出力 (jq / `input=$(cat)` / hookSpecificOutput)・
  stat / lockf / mkdir lock・XDG / .cache・date -j / shasum・osascript / pbcopy・die / usage・`tail -n` の刈り込み・ffprobe・command -v・worktree

## 寄せたもの

1. **観測ログの既定パス (不具合)**: `TT_TRIGGER_LOG` の既定 (`$HOME/.cache/tt-restore-trigger.log`) を 9 本が手で書き、**tmux_resurrect_save.sh だけが書いていなかった**。
   set -u の未定義が tt_trigger_log の `|| true` に潰され、保存の wrapper の記録 (save-advanced / regression-blocked など) が本番のログに **2026-08-25 16:52 (issue 079 の集約の直後) から 1 行も書かれていなかった**
   (2026-10-16 に実測。周期保存の記録は続いていた。保存そのものは止まっていない)。テストは常に TT_TRIGGER_LOG を渡すので見えなかった
   → 既定を guards.sh の読み込み時に 1 か所で決め、8 本の手書きを外した。保存の wrapper のテストに「TT_TRIGGER_LOG を渡さずに既定の置き場へ書く」場合を足した
2. **行数でログを刈る処理**: tmux_periodic_save.sh の `prune_trigger_log` と tmux_schedule_keys.sh の `log_rotate` が同じ 4 行 → guards.sh の `tt_prune_log_lines`
3. **resurrect の保存先の解決**: zshlib/_tmux_session.zsh の `_tt_wait_for_restore` が guards.sh を読み込んでいるのに `tt_resurrect_dir` を丸ごと書き直していた → `tt_resurrect_dir` を使う。
   `_tt_gc_stale_holds` の `${TT_HOLD_PREFIX:-__tt_hold_}` (guards.sh が必ず決める値の既定の写し) と、tmux_snapshot_health.sh の到達しない予備の解決も外した
4. **保存の実行と最終保存時刻の簿記・保存の間隔**: `tt_run_resurrect_save` (debounce 側) と周期保存の手書きが同じ簿記 → guards.sh へ移し、wrapper が無い (rc=2) と失敗 (rc=1) を分けた。
   `@continuum-save-interval` を読んで既定 15 分に落とす処理 (周期保存・snapshot_health) → `tt_save_interval_minutes`
5. **hook の入出力**: stdin → jq の有無 → `.tool_input.command` の 3 行 (5 本) と、deny の JSON の 9 行 (2 本) → `_claude/hooks/lib/hook_io.sh` (`hook_read_bash_command` / `hook_deny`)。どれも fail-open のまま
6. **zsh のファイルサイズ**: av1ify の `__av1ify_file_size` と同じ `stat -f%z … || stat -c%s …` を concat が 3 か所で手書き → `_fs_helpers.zsh` の `__fs_file_size` (GNU の分岐は macOS 専用の方針で落とした)

## 寄せないもの (理由)

- **整数の検査を bin/lib/uint.sh に**: 利用者の入力を受けるのは scripts/clean_tmp.sh の `DAYS` だけ (bash 3.2 の `[!0-9]` が全角を通すが、後段の find が rc=2 で止める)。ほかの約 25 か所は tmux が出す値が相手。
  scripts / hooks から bin/lib をディレクトリをまたいで読む費用に見合わない
- **ffprobe の csv の先頭行**: _ffprobe_helpers.zsh の冒頭が「csv=p=0 は形が違うので各所に残す」と決めている
- **自分の置き場の解決 (約 40 か所)**: lib を読む前の 1 行で、helper にすると鶏と卵になる。CDPATH の罠は check_cdpath_before_cd_capture.sh が止めている
- **tmux_agent_panel.sh の mkdir lock**: 経路ごとに lock の方針を分けると issue 078 と guards.sh の `tt_lock_acquire` の注記で決まっている
- **schedule_keys の refresh_* の job の手読み**: `read_job` を使うと呼び出し側の REPLY_* を壊す (cancel_job の注記と同じ理由)
- **tmux-pane-working.sh と tmux-pane-state.sh の working 分岐**: 起動時間のために意図して分けている (issue 622)
- **tmux_reap_orphan_servers.sh の tt_trigger_log と TT_TRIGGER_LOG の手書き**: `#!/bin/sh` で guards.sh を読まない例外 (`trigger-log-writer: allow`)。
  理由の「/bin/sh は dash」は macOS 専用になって古いが、寄せるかは別の判断
- **fail / die / usage・`date +%s`・`command -v X || exit 0`・additionalContext の組み立て**: 1 行の定型か、hookEventName と追加の欄が hook ごとに違う

## 進捗

- [x] 実装 (commit:「fix(tmux): 観測ログの既定の置き場を guards.sh 1 か所に…」/「refactor(tmux): 行数でログを刈る処理を…」/「…zsh の _tt_wait_for_restore の保存先の解決を…」/
  「…保存の実行と最終保存時刻の簿記・保存の間隔の解決を…」/「test(tmux): 保存の間隔が 0・数字でないときに…」/「refactor(zsh): ファイルサイズの取得を…」/「refactor(hooks): Bash の hook の入力の読み取りと deny の出力を…」)
- [x] 変異 (mutate-verify。全部 red): guards.sh の既定を外す → 保存の wrapper のテストの既定の置き場 / 刈り込みの上限の比較を外す → 周期保存のテスト /
  zsh の保存先を固定のパスに → test_tt.sh / tt_run_resurrect_save の rc=2 を 1 に → 「skipped=no-save-script が記録されない」/ 最終保存時刻の set-option を外す → 周期保存のテスト /
  間隔 0 の既定への寄せを外す → 足したテスト (寄せる前から間隔 0 はどのテストも見ていなかった) / `__fs_file_size` を空にする → concat 1 本・av1ify 3 本 /
  hook_deny の permissionDecision を allow に → deny の 2 本 / 読み取る欄を変える → git-state-verify ほか
- [x] 関係するテスト: tests/tmux の periodic_save・schedule_keys・snapshot_health、tests/zshrc/tmux-session の 6 本、concat・av1ify の 36 本、tests/claude の hook 5 本は rc=0
- [x] 敵対的レビュー (opus・read-only): **P2 1 件**: `_tt_gc_stale_holds` から `${TT_HOLD_PREFIX:-__tt_hold_}` の既定を「guards.sh が必ず決める値の写し」として外したが、
  これは破壊的な誤爆の最後の砦を兼ねていた。prefix が空 (guards.sh を読めなかった等) だと全セッションに一致し、"0" のような数字名のセッションを死んだ hold と読んで kill する
  (隔離サーバで再現) → prefix が空なら何もしない guard を関数の頭に置き、test_tt.sh に固定した (guard を外す変異で「kill=[ =0]」が出て red)。
  P3: HOME 未設定 + set -u で guards.sh の読み込みが落ちる → `${HOME:-}` / コメントの「既定はここだけ」に reap の例外 / **前からの不具合**: 間隔が "08" だと 8 進として読まれ、
  算術が壊れて周期保存が待たずに回った → 10 進に直してテストを足した / `hook_read_bash_command` は jq が値を出した後に失敗する入力 (JSON の後ろにゴミ) を素通りにする →
  Claude Code は単一の JSON を送るので発火経路が無く、deny 系は「誤 deny させない」fail-open なので受容。
  壊せなかった: TT_TRIGGER_LOG の zsh への漏れ (export されない)・名前の衝突・tt_run_resurrect_save の rc の意味・保存先の解決 (空白・~・$HOME・$HOSTNAME で旧と一致)・
  symlink 越しの hook の起動・__fs_file_size の空の扱い・寄せないものの判断。
  テストの穴 (前から): schedule_keys の刈り込みと snapshot_health の間隔 0 はテストが見ていない
- [x] make test (776 と 777 を合わせた木・rebase の後): go-lint 23 / go-test 22 module rc=0。赤は 2 つ: test-shellcheck (hook_io.sh の HOOK_INPUT / HOOK_CMD を SC2034 が未使用と見た
  → 理由つきで抑止。make test-shellcheck rc=0) / restartable の TestPTYRunnerDrainsLateOutputDuringTerminationWithInputBurst (restartable は触っていない。単独で 3 回 rc=0。並列の負荷の下での不安定さ)
