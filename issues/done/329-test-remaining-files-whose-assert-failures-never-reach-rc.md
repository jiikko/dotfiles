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

---

## 対応結果 (2026-09-08)

### todolist（受け入れ条件）

- [x] 「出ない」と判定されたファイルを fail カウンタ方式（または即 `exit 1`）へ揃える
- [x] 「判定不能」のファイルは、なぜ判定できないかを 1 件ずつ確かめて
      「道具の射程外」か「本当に rc に出ない」かを分ける
- [x] 各ファイルについて、**中盤のアサーションを 1 つ落とす変異**で rc が非 0 になることを確認する
- [ ] `make test-assert-reaches-exit` が緑になり、Makefile と `tests/CLAUDE.md` の
      「当面は赤いのが正常」を消す — **未達**。判定不能 16 件が残るので rc は 1 のまま
      （道具の射程の問題であって、テスト側の欠陥ではない。下記）

### 🚨 まず: 上に貼ってあった母集合は**私のツールの偽陽性を含んでいた**

着手して 18 件を手で検算したところ、**半分が偽陽性**だった。原因は 3 形とも
「**注入した分岐へ到達していないのに rc=0 を『rc に出ない』と読んだ**」形:

| 形 | 内容 | 誤判定したファイル |
|---|---|---|
| ① 連鎖 | `if A; then … elif B; then … else ✗ fi` で elif だけ false にしても A が真なら else へ入らない | `test_deny_bare_tmux_kill.sh` / `test_next_claim_push.sh` |
| ② 1 行 else | `else echo '✗ …'; fi` は ✗ 行自身が else なので「then 側」と誤読して `if true` にしていた | `test_warn_discarding_checkout.sh` |
| ③ 行継続 | `cmd \` + `  \|\| { printf '✗ …'; exit 1; }` の形を扱えず注入が空振り | tmux の 5 件 / `test_ai_commands.sh` |

直しの本命は **「注入した分岐に本当に入ったかを出力（`✗` の数の増分）で確かめる」**
（`0e0ce542`）。到達していない候補は次の候補へ回し、1 つも到達しなければ判定不能にする。
併せて ①〜③ の抽出も直し、候補を **最初 / 中央 / 最後**の 3 箇所で試すようにした。

🚨 ついでに `xmarks` を `export -f` し忘れて `[ "" -le N ]` が「integer expected」で非 0 になり、
**到達していない候補を「到達した」と読む**形も自分で作った（空文字を返さないようにして塞いだ）。

**教訓**: 自作の検査が赤を出したら、**その赤を「既知の正解」で検算するまで作業リストにしない**。
今回は「直したばかりの `test_av1ify_prefetch.sh`」が canary として効いたが、
それを持っていなければ 9 件の偽陽性を作業リストとして配っていた。
（切り出し先は retro 328 の項目 D-2 に集約済み。ここでは実測だけ残す）

### 訂正後の母集合（実測 2026-09-08、`0e0ce542` のツール）

```
検査 116 件 / 結果を報告 116 件: rc に出る 61 / 出ない 9 / 判定不能 16 / ✗ を出さない 30
```

「出ない」は **9 件**（18 件のうち 9 件が偽陽性だった）。すべて av1ify / concat 配下。

### 直した内容

**共通ヘルパーに寄せた**（`tests/zshrc/av1ify/test_helper.sh` / `tests/zshrc/concat/test_helper.sh`）。
両ディレクトリのテストは helper を source するので、1 箇所で全ファイル（今後書かれるものを含む）に効く:

```zsh
typeset -gi FAIL_COUNT=0
bad() { printf "$@"; FAIL_COUNT=$(( FAIL_COUNT + 1 )); }

cleanup() {                 # 既存の EXIT trap に束ねる (trap は 1 つしか持てない)
  local rc=$?
  rm -rf "$TEST_TMP"
  if (( FAIL_COUNT > 0 )); then printf '✗ 失敗 %d 件 …\n' "$FAIL_COUNT" >&2; exit 1; fi
  return $rc                # 本体が既に非 0 なら格下げしない
}
```

zsh の EXIT trap 内の `exit 1` が終了ステータスを上書きできることは実測で確かめた
（`FAIL_COUNT>0` → rc=1 / `FAIL_COUNT=0` かつ本体 `exit 1` → rc=1 のまま）。

各ファイルは `printf '✗ …'` → `bad '✗ …'` の**コマンド名の置き換えだけ**（書式も引数もそのまま）:

| ファイル | 置き換え |
|---|---|
| `av1ify/test_av1ify_audio.sh` | 3 |
| `av1ify/test_av1ify_avsync.sh` | 5 |
| `av1ify/test_av1ify_basic.sh` | 1 |
| `av1ify/test_av1ify_color_tags.sh` | 2 |
| `av1ify/test_av1ify_options.sh` | 14 |
| `av1ify/test_av1ify_postcheck.sh` | 20 |
| `av1ify/test_av1ify_variants.sh` | 1 |
| `concat/test_concat_edge.sh` | 13 |
| `concat/test_concat_output_info.sh` | 5 |
| **計** | **64** |

### 変異検証

🚨 **「カウンタ経路だけが落ちる」変異**を選ぶのが要点。素朴に壊すと `assert_*`（`return 1` →
`err_exit`）が先に落ちて、**新しく足した仕掛けを 1 mm も検査しないまま rc=1 になる**。

| 変異（production 側） | 落ちた位置 | 走り切ったか | rc |
|---|---|---|---|
| `__concat_is_allowed_ext` が mkv / webm を弾く（mp4 は残すので手前の `assert_file_exists` は素通り） | 中盤 `bad` ×2 | **最後まで走り切って** `✗ 失敗 2 件` | **1** |
| `_av1ify.zsh` の無効な `--color-tags` が `return 0`（メッセージは出るので `assert_contains` は素通り） | 中盤 `bad` ×1 | **最後まで走り切って** `✗ 失敗 1 件` | **1** |

参考（**採用しなかった**変異 2 本）: `__concat_is_allowed_ext` を全部弾く / postcheck が常に
サイズ増加を警告する — いずれも rc=1 になるが、落ちたのは `assert_*` で `err_exit` が先に効いており、
カウンタ経路を検査していない。

### ツールでの確認

```
tests/zshrc/av1ify tests/zshrc/concat: 検査 30 件 / 報告 30 件
  rc に出る 20 / 出ない 0 / 判定不能 1 / ✗ を出さない 9
```

残る 1 件は `test_av1ify_validate.sh`（別セッションが 2026-09-08 に追加。`&& … \` + `|| { …; exit 1; }`
の形で、**rc には出る**が道具が条件を固定できない）。

### 判定不能 16 件のトリアージ — **すべて「道具の射程外」で、rc に出る仕掛けは持っている**

| 仕掛け | ファイル |
|---|---|
| `fail` カウンタ + 末尾 `exit` | `bin/test_go_autobuild_warmup.sh` / `scripts/test_no_failfast_entrypoints.sh` / `setup/test_terminal_profile_colors.sh` / `setup/test_terminal_profile_restore.sh` / `theme/test_theme_colors.sh` / `tmux/test_ctrl_v_paste.sh` / `tmux/test_mark_seen.sh` / `tmux/test_agent_jump.sh` / `tmux/test_agent_panel.sh` / `zshrc/tmux-session/test_resurrect_{lock_acquire,owner_fingerprint,save_archive}.sh` |
| ✗ の直後に `exit 1` | `nvim/test_ftplugins.sh`（ハーネスの故障を告げる ✗）/ `zshrc/av1ify/test_av1ify_validate.sh` |
| `fail()` 関数が `exit 1` | `test_bench_stats.sh` / `zshrc/codex-wrapper/test_codex_snapshot_survives.sh` |

判定できない理由は 2 つ: **①`✗` が `if/else` にも `\|\|` にも載っていない**（`case` の中 /
関数の中 / `&&` 連鎖）**②候補の分岐へ到達させられない**（`tmux/test_agent_{jump,panel}.sh`)。
**静的確認であって実測ではない**ので、そこは未検証として残す。

### 「✗ を出さない 30 件」のうち 16 件（`FAIL:` / `ERROR` で報告）— こちらも仕掛けは持っている

`claude/` の 8 件はいずれも `fail` カウンタ + 末尾 `exit`、`nvim/` の 4 件は `exit 1`、
`tmux/` の 4 件は `fail()` 関数 + `exit 1`。こちらも**静的確認**。
道具のマーカーを機械的に広げると `MOCK_FFMPEG_FAIL` のような変数名に当たるので、広げていない。

### 残タスク

- **`make test-assert-reaches-exit` は赤のまま**（判定不能 16 件）。是正すべきテストは
  無くなったので、次に進むなら **道具の射程を広げる**方（`case` / `&&` 連鎖 / 関数内の ✗ を
  扱う、あるいはマーカーを設定可能にする）で、テスト側の作業ではない
- 判定不能 16 件 + 別マーカー 16 件の「rc に出る」は**静的確認どまり**（実測していない）
