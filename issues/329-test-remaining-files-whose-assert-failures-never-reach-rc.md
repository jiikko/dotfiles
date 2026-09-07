# test: アサーションの失敗が exit code に出ないテストの残り (327 の母集合)

起票日: 2026-09-08
カテゴリ: test
優先度: 中（**失敗が緑として集計される**形。1 ファイルずつ潰す）
出典: issue 327（機構の是正と棚卸しの道具はそちらで完了）

## 何が残っているか

issue 327 で

- `tests/zshrc/av1ify/test_av1ify_prefetch.sh` を fail カウンタ方式へ是正した
- 母集合を実験で確定する道具 `scripts/check_assert_reaches_exit.sh`
  (`make test-assert-reaches-exit`) を足した
- 規約を `tests/CLAUDE.md`「アサーションの失敗は exit code に出す」に書いた

残っているのは **その道具が「出ない」と判定したファイルの是正**。母集合は下の節に
実測で貼ってある（**この一覧は棚卸しの結果であって、赤いこと自体は既知**。
`make test-assert-reaches-exit` が赤いのを「自分が壊した」と読まないこと）。

## 母集合（実測 2026-09-08。`make test-assert-reaches-exit`、PROBE_JOBS=4 / PROBE_TIMEOUT=300）

```
検査 115 件 / 結果を報告 115 件: rc に出る 55 / 出ない 18 / 判定不能 12 / ✗ を出さない 30
```

### 出ない = アサーションが落ちても rc が 0 のまま（18 件）

| ファイル | 実測で rc に出なかった行 |
|---|---|
| `tests/claude/test_deny_bare_tmux_kill.sh` | 26 |
| `tests/claude/test_next_claim_push.sh` | 24 |
| `tests/claude/test_warn_discarding_checkout.sh` | 194 |
| `tests/tmux/test_extract_popup.sh` | 62 |
| `tests/tmux/test_log_kill_command.sh` | 210 |
| `tests/tmux/test_log_restore_hook.sh` | 45 |
| `tests/tmux/test_restore_runner.sh` | 110 |
| `tests/tmux/test_server_watchdog.sh` | 94 / 195 |
| `tests/zshrc/ai-commands/test_ai_commands.sh` | 34 |
| `tests/zshrc/av1ify/test_av1ify_audio.sh` | 288 / 340 |
| `tests/zshrc/av1ify/test_av1ify_avsync.sh` | 200 |
| `tests/zshrc/av1ify/test_av1ify_basic.sh` | 198 |
| `tests/zshrc/av1ify/test_av1ify_color_tags.sh` | 51 / 148 |
| `tests/zshrc/av1ify/test_av1ify_options.sh` | 54 / 750 |
| `tests/zshrc/av1ify/test_av1ify_postcheck.sh` | 37 |
| `tests/zshrc/av1ify/test_av1ify_variants.sh` | 177 |
| `tests/zshrc/concat/test_concat_edge.sh` | 70 |
| `tests/zshrc/concat/test_concat_output_info.sh` | 248 |

🚨 **行番号は sweep 時点のもの**。ファイルを編集したらずれるので、位置ではなく
「そのファイルが rc を返す仕掛けを持っているか」で直すこと（道具を回し直せば新しい行が出る）。

### 判定不能（12 件）— 合格でも不合格でもない

いずれも「`✗` へ入る条件を固定できる分岐が無い（書き方が想定外）」。
道具が扱えるのは `if COND; then ✓ else ✗ fi` と `COND || <報告>` の 2 形だけなので、
`case` / 関数呼び出しだけの assert / 複数行にまたがる条件はここへ落ちる。
**「rc に出る」とは言えていない**ので、1 件ずつ人が見る必要がある。

```
tests/bin/test_go_autobuild_warmup.sh
tests/scripts/test_no_failfast_entrypoints.sh
tests/setup/test_terminal_profile_colors.sh
tests/setup/test_terminal_profile_restore.sh
tests/test_bench_stats.sh
tests/theme/test_theme_colors.sh
tests/tmux/test_ctrl_v_paste.sh
tests/tmux/test_mark_seen.sh
tests/zshrc/codex-wrapper/test_codex_snapshot_survives.sh
tests/zshrc/tmux-session/test_resurrect_lock_acquire.sh
tests/zshrc/tmux-session/test_resurrect_owner_fingerprint.sh
tests/zshrc/tmux-session/test_resurrect_save_archive.sh
```

### 🚨 「✗ を出さない 30 件」は合格ではない — 道具の射程外

道具は `✗` の行しか探さない。**そのうち 16 件は `FAIL:` / `ERROR` など別のマーカーで
失敗を報告している**（実測 2026-09-08）ので、**一度も検査されていない**。
マーカーを機械的に増やすと `MOCK_FFMPEG_FAIL` のような変数名にも当たるため、
道具側は広げず、ここに残債として置く。

```
tests/claude/test_claude_links_complete.sh      tests/claude/test_claude_links_sync.sh
tests/claude/test_codex_drive_effort_consistency.sh  tests/claude/test_dangling_symlinks.sh
tests/claude/test_human_tasks_due.sh            tests/claude/test_issue_progress_check.sh
tests/claude/test_retro_open.sh                 tests/claude/test_skill_trigger_table.sh
tests/nvim/test_folds_timer.sh                  tests/nvim/test_image_hover.sh
tests/nvim/test_nvim.sh                         tests/nvim/test_smooth_scroll.sh
tests/tmux/test_fork_scratch.sh                 tests/tmux/test_reap_orphan_servers.sh
tests/tmux/test_smooth_scroll_unit.sh           tests/tmux/test_smooth_scroll.sh
```

（残りの 14 件は失敗の報告そのものが無いか、別の形で rc を返している）

## 進め方の案

- **1 ファイルずつ**。`tests/zshrc/av1ify/test_av1ify_prefetch.sh`（issue 327 で是正済み）が参照実装
- 直したら `scripts/check_assert_reaches_exit.sh <そのファイル>` で「rc に出る」になることを確認し、
  さらに**中盤のアサーションを 1 つ落とす変異**で rc が非 0 になることを見る
- 全部片付くまで `make test-assert-reaches-exit` は赤い。これは既知の状態
- 選択肢: 母集合を baseline ファイルに固定して「新規に増えたものだけ落とす」ラチェットにする案もある。
  ただし `make test` に入れていない（重い）ので自動では効かず、baseline が腐るコストと引き合うかは未判断

## 受け入れ条件

- [ ] 「出ない」と判定されたファイルを fail カウンタ方式（または即 `exit 1`）へ揃える
- [ ] 「判定不能」のファイルは、なぜ判定できないかを 1 件ずつ確かめて
      「道具の射程外」か「本当に rc に出ない」かを分ける
- [ ] 各ファイルについて、**中盤のアサーションを 1 つ落とす変異**で rc が非 0 になることを確認する
      （末尾だけで確認すると、カウンタが末尾にしか無い形を見落とす）
- [ ] 全部片付いたら `make test-assert-reaches-exit` が緑になることを確認し、
      Makefile と `tests/CLAUDE.md` の「当面は赤いのが正常」を消す

## 関連

- issue 327（機構・道具・規約。**この issue は残債の消化だけ**）
- issue 139（丸ごと skip が `[ok]` になっていた先行事例）
