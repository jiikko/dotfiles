# 072 research: テストコード監査 (2026-08-20)

起票日: 2026-08-20

`/audit` テストバッチ (test-cleanup / test-helpers) の結果。forge Standard × 7 エージェント +
test-coverage-advisor。統合フェーズは session limit で落ちたため main agent が集約した。

エージェントには「どの変異を当てればそのテストが green のままだと示せるかを書けない指摘は
出すな」「回帰防止テストを低価値に分類するな」を渡してある。全体評価は
「水準は高い (変異検証の痕跡がコメントに残り、stub は実物と一致、false-green ガードが多重)」
で、無価値テストは少数だった。

**✓ は main agent がコードで存在確認した項目。それ以外は未裏取り。**

## 検証対象を skip 条件にしているテスト

- ✓ **`src/glogx/scroll_glide_test.go:150` / `:328`** — `t.Skip("この geometry では半ページで
  動かない (テスト前提の破れ)")`。geometry は `newTestBrowse(t, 30, ...)` + `m.height = 12` で
  テスト内に完全に決定論的に組まれているので、offset が動かないのは環境要因ではなく
  **実装が壊れた場合だけ**。つまり「壊れたら skip して緑」という構造。
  変異: `tui.go` の ctrl+d 経路を no-op にすると本来 red になるべきところが skip で緑
- ✓ **`tests/tmux/test_fork_scratch.sh:118` / `:197`** — 検査 A と F が「fork popup は現在
  無効」で恒久 skip。無効化は 2026-06 の A/B 観測が理由で、その観測はもう終わっている
  (`_tmux.conf` の bind b はコメントアウトのまま)。復活させるか検査を畳むかの判断が必要
- **`tests/nvim/test_ftplugins.sh:149-162`** — `]]` の実解決検査が treesitter parser の有無で
  無音 skip
- ✓ **`tests/zshrc/test_zshrc.sh:109-116` (Test 6)** — pyenv は `_zshrc` / `_zprofile` /
  `_zlogin` / `zshlib/` のどこにも存在しない (repo 全体で pyenv を含むのはこのテストだけ)。
  host gate 付きなので手元でも CI (ubuntu) でも常に skip。**同ファイルの Test 4 が同型の
  false-pass を修正済みと記録している**のに Test 6 に残っている

## 陽性対照 (下界) が無いテスト

- ✓ **`tests/nvim/folds_timer_check.lua:35`** — uv timer リークの検査が `delta > 2` の
  上界だけ。timer が **1 本も張られていない** (= debounce が丸ごと死んでいる) 状態が
  最も good に見える。下界 (1 本張られたこと) の assert が必要
- ✓ **`tests/zshrc/lazy-loading/test_version_managers.sh:118-132`** — lazy load の本体
  (`unfunction` → `eval "$(tool init -)"` → 再ディスパッチ) が一度も実行されていない。
  assert は「関数として定義されているか」「`*_ROOT`」「PATH 順」の 3 種だけ。fixture が
  `init -` で `${UPPER}_INIT_CALLED=1` を吐く用意を既にしているのに読む assert が無い。
  変異: `_zshrc` の eval 文字列を空関数に置き換えても 12 assert 全部が緑
- ✓ **`tests/_ensure_cli_with_brew.bats:60-71`** — 唯一の assert が「ログファイルが無いこと」。
  `$status` も見ておらず、関数が実行されたことを示す陽性対照が無い。エージェント実測:
  関数本体を `return 0` に置き換えても このテストは緑 (別のテストは red になる)。
  副次: `:43-44` の `export -f brew` / `export -f type` は死んだコード
  (bash の関数 export は `zsh -c` の子に継承されず、実際の注入は `declare -f` の文字列展開)
- **`src/parallel-each/result_log_test.go:101-104`** — assert が 1 つも無い
  (「panic しないこと」だけ)。production の doc が主張する不変条件を守っていない

## 常に真 / 自己言及になっているアサーション

- ✓ **`tests/zshrc/av1ify/test_av1ify_options.sh:663-691` (Test 72)** — 5 本の
  `assert_contains` が `unsetopt err_exit` … `setopt err_exit` の窓の内側にある。
  helper の `assert_contains` は失敗時 `return 1` するだけなので、この窓の中の失敗は
  全体の rc に反映されない
- ✓ **`tests/zshrc/concat/test_helper.sh:194`** — `export CONCAT_DURATION_TOLERANCE=100` が
  production 既定 (`_concat_helpers.zsh:425` の `:-5`) を上書きしており、duration 乖離検査と
  サイズ乖離検査が concat テスト 14 ファイルのどれでも発火しない
- ✓ **`tests/tmux/test_agent_panel.sh:166`** — `grep -q -- '-t @2'` は CALLS ログ全体への
  部分一致で、同スクリプトの `display-message -p -t "$win"` が同じ文字列を書くため、
  new-pane の target とは無関係に必ずマッチする
- **`src/glogx/open_workspace_test.go:40`, `:62`** — `want := repoRoot()` と production 関数を
  期待値の出典にする自己言及 (コメント自身が「repoRoot() を使う」と書いている)
- **`src/glogx/issues_view_test.go:2057`** — 期待値を production と同じ式から組む自己言及
- **`tests/codex_fanout.bats:136`** — `[ "$status" -eq 1 ]` だけで、どの理由で 1 になったか
  (mode 不正 / label 重複) を区別していない
- **`tests/claude/test_deny_bare_tmux_kill.sh:19-22`** — decision 抽出が
  `grep . || echo allow` で、フックの異常終了と allow を区別できない。069 のバイパスが
  このテストを素通りした背景
- **`tests/claude/test_dangling_symlinks.sh:20-39`** — 対象リンクが 0 件のとき
  「OK: dangling symlink なし」で exit 0。CI (dotfiles 未 symlink) では常にこの経路。
  `adversarial-review-own-safeguards.md` が禁じる「検査できなかったときに緑」。
  対応は削除ではなく `SKIP: 対象リンク 0 件` への表示是正

## ヘルパー共通化 (test-helpers)

既に `tests/tmux/lib/stub_assert_helper.sh` と `lib/isolate_env.sh` への集約は済んでいるが、
その外に verbatim 重複が残っている。

- ✓ **nvim の headless ラッパー 3 本が完全同一** — `test_smooth_scroll.sh:20-39` /
  `test_folds_timer.sh:18-37` / `test_image_hover.sh:19-37` (ラベルとコメント以外一致)。
  **しかも既にドリフト済み**: image_hover だけ `grep -q "OK"` (アンカー無し)、他 2 本は
  `grep -q "^OK"`。`tests/nvim/lib/check_log.sh` のヘッダが「かつて各テストへコピペされ
  貼り忘れが実 false-pass を起こしたので一元化した」と明記しているのに、この 3 本には
  適用されていない (同型バグの横展開漏れ)
- **偽サーバ生成ヘルパーが 3 ファイルに同一実装** — `test_periodic_save.sh:35-39` /
  `test_snapshot_health.sh:29-33` / `test_server_watchdog.sh:26-30`。load-bearing な
  ⚠️ コメント (素の `cmd &` だと EXIT trap 継承で TMP_DIR がテスト途中で消える。
  2026-07-30 に特定) までコピペされている
- **`DEFAULT_SOCK="$(realpath /tmp ...)/tmux-$(id -u)/default"` が 5 ファイルに逐語コピー**
- **`assert_contains` が 8 コピー、`assert_file_exists` が 5 コピー** — `tests/zshrc/**` の
  各 test_helper.sh。av1ify / concat / その他で独立実装
- **stub 方式のプレリュード (`STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"` 等) が未抽出**
- **`test_agent_panel.sh` / `test_agent_jump.sh` / `test_tmux_toast.sh` の 3 本だけが
  共有 lib を使わず自前 ok/ng を持つ** — ただし設計差がある (共有 lib は fail-fast、
  この 3 本は fail を積んで最後に報告) ため素の置換は不可。共有 lib に accumulate 版
  (`expect_called` + `tt_assert_summary`) を足して移行するのが構造的解消
- **`tests/tmux/test_tmux_toast.sh:160,172`** — `out=$(...); rc=$?` を `set -e` 下で使うと
  代入失敗の時点で死んで診断が出ない。同ディレクトリの `test_extract_popup.sh:56` が
  同じ class を既に潰している (共有 lib の `run()` を使う)

## 攻めて見つからなかった範囲

- tmux stub 方式テスト 11 本の stub は production の呼び出し形と一致していた
  (実物とずれた stub は `test_snapshot_health.sh` の lock fixture のみ → 068)
- glogx の Go テストは自己言及 2 件を除き、期待値を独立に構成していた

---

## 反証・対応の結果 (2026-08-21)

⚠️ **この節が唯一の durable な記録**。裏取り/反証のレポートは `./tmp/` に出したが `tmp/` は
gitignore 対象なので残らない。以降の audit はこの節を先に読むこと (却下済みの指摘を再生成しない)。
「反証できなかった」は「正しいと証明された」ではない。

### 対応済み

| 指摘 | 対応 |
|---|---|
| `scroll_glide_test.go:150,:328` の `t.Skip` 逃げ | `d5e16d4`(=`9e78c43`) で `t.Fatal` へ。変異 (`pageSize()/2` を 0 に) で 2 テストが red = **修正前は browse の半ページスクロールが完全に死んでも `ok glogx`** だった |
| `test_av1ify_options.sh:663-691` の `err_exit` 窓 | `0a863db` で窓を削除。変異 (最長一致を壊す) で red。この変異は **SMB 上の元ファイルに `trash` が呼ばれる**実データ削除経路そのもので、窓があった間は完全に緑だった |
| `codex_fanout.bats:136` が status 1 の理由を区別しない | `09de900` で落ちた理由をメッセージで pin。変異 2 種で red |
| `test_deny_bare_tmux_kill.sh:19-22` の decision 抽出 | 並行セッションが `7c064e6` で error と allow を分離。さらに**入力長で hook が timeout に殺され deny が消える**穴を私が実測し `0ceb0f8` で修正 (テスト側が本番と同じ `timeout: 10` を課していなかったため 4 ケース書いても全部緑だった) |
| `test_agent_panel.sh:166` の `grep -q -- '-t @2'` が常にマッチ | `7f83abb` (follow 側) + `512b490` (save-show 側)。new-pane 行限定へ。変異 (new-pane から target を落とす) で red |
| `folds_timer_check.lua:35` の上界のみ | `512b490` で下界を追加。変異 (debounce を殺す) で red = **修正前は機能が丸ごと死んだ状態が「最も good」に見えていた** |
| `test_dangling_symlinks.sh` の 0 件緑 | **却下** (下記) |
| `concat/test_helper.sh:194` の tolerance 上書き | **半分却下**。生き残った部分を issue 076 として分離 (素朴に tolerance を戻すと false failure になる) |

### 反証で崩れた (却下) — 再提案しないこと

| 指摘 | 崩れた理由 (要点) |
|---|---|
| `test_dangling_symlinks.sh` が対象 0 件で緑 | 0 件素通しはヘッダ (`:13`) と導入 commit (`baa98c5`) が**宣言済みの性質**。守る対象があるマシンでは母集団が空にならず rc=1 で検出される (実測 90 本)。0 件になる環境では不変条件が「未検証」ではなく**真**。引用も現物 (「OK: dotfiles 由来の dangling symlink なし」) と不一致で過大主張だった |
| `assert_contains` の 8 コピーが fail-fast を壊す | 5 本すべて `setopt err_exit` を立てており `return 1` で後続未実行・rc=1 を実測 (裏取り側の `grep '^\s*set -[eu]'` が zsh の `setopt` を拾えなかった誤認)。引数順の誤りは**赤で loud** に落ちる。live な誤 callsite 0 件 |
| `_ensure_cli_with_brew.bats:60` に陽性対照が無い | 主張 (早期 return が効く) を壊す変異では `not ok 2` で red。残るのは `$status` 未検査の 1 行ぶんのみ。FALSE_POSITIVE |
| `issues_view_test.go:2057` の自己言及 | 指摘行は無関係な行。該当式はキー側/描画側の 2 経路一致検査で、分岐を壊す変異では FAIL する。FALSE_POSITIVE |

### 未着手 (発火条件を示せない / trigger 待ち)

`072-lazy-body-untested` (trigger: fixture の unquoted heredoc 修正と同時。単独で assert を足すと
誤った期待値を固定する) / `072-nvim-wrapper-dup` (trigger: 4 本目の nvim テストを足すとき) /
`072-ftplugin-ts-silent-skip` / `072-pyenv-always-skip` / `072-open-workspace-selfref` /
`072-result-log-no-assert` / `072-brew-bats-dead-export` / `072-fork-scratch-perma-skip` /
`072-tmux-spawn-helper-dup` / `072-default-sock-dup` / `072-assert-file-exists-dup` /
`072-three-own-ok-ng` / `072-toast-set-e-rc` / `072-nvim-ok-anchor-drift`。

### この監査で得た一般則 (ルールへ反映済み)

今回の修正過程で**同型の失敗を私自身が 3 回踏んだ**ため、規範に落とした:

- `adversarial-review-own-safeguards.md`: 新設した検査が **CI で実際に走るか**を同じ commit で
  確認する (孤児回収の静的検査が `lsof` gate の下で一度も走っていなかった実例) /
  **テストハーネス自身の失敗も緑に畳まない** (jq が ARG_MAX で落ちたのを allow に畳み、CI ログが
  「判定の誤り」に見えた実例)
- `mutation-verify-new-tests.md`: **本番と同じ制約 (timeout 等) の下で観測する** (hook のテストが
  `timeout: 10` を課さず、4 ケース書いても変異で緑だった実例)


---

## 未着手残債の切り出しと done 化 (2026-08-21)

この監査の未着手項目 14 件はすべて独立 issue へ切り出したので、**この issue は done へ移す**。
以降この節から辿ること (「反証で崩れた (却下)」の一覧は**この issue が唯一の記録**なので、
同型の指摘を再生成しかけたら必ず上へ戻って読む)。

| 未着手 id | 切り出し先 |
|---|---|
| `072-nvim-wrapper-dup` / `072-nvim-ok-anchor-drift` / `072-tmux-spawn-helper-dup` / `072-default-sock-dup` / `072-assert-file-exists-dup` / `072-three-own-ok-ng` / `072-toast-set-e-rc` | [081](../081-refactor-test-helper-verbatim-dup.md) |
| `072-pyenv-always-skip` / `072-fork-scratch-perma-skip` / `072-ftplugin-ts-silent-skip` / `072-result-log-no-assert` / `072-open-workspace-selfref` / `072-brew-bats-dead-export` / `072-lazy-body-untested` | [082](../082-bug-vacuous-tests-remaining.md) |

`072-concat-tolerance-drift` の生き残りは既に [076](../076-bug-concat-duration-check-never-fires.md)。
