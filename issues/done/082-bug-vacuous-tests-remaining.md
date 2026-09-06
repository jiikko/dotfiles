# 082 bug: 主張を守っていないテストの残り (常に skip / assert 不在 / 自己言及)

起票日: 2026-08-21
種別: bug
優先度: **P3** (production は正しい。テスト側が空回りしているだけ)

出典: 監査 [072](072-research-test-audit-2026-08-20.md) の `072-pyenv-always-skip` /
`072-fork-scratch-perma-skip` / `072-ftplugin-ts-silent-skip` / `072-result-log-no-assert` /
`072-open-workspace-selfref` / `072-brew-bats-dead-export` / `072-lazy-body-untested`。
同じ監査で**修正済みのもの** (`scroll_glide_test.go` の `t.Skip` / `test_av1ify_options.sh` の
`err_exit` 窓 / `folds_timer_check.lua` の下界 / `test_agent_panel.sh` の部分一致 ほか) は
ここに含めない。**出典 issue の「反証で崩れた (却下)」一覧** (`test_dangling_symlinks.sh` の
0 件緑 / `_ensure_cli_with_brew.bats` の陽性対照 / `issues_view_test.go:2057` の自己言及) は
**再提案しないこと**。

## 確認できた事実 (2026-08-21)

- **`tests/zshrc/test_zshrc.sh:110-116` (Test 6)** — pyenv は `_zshrc` / `_zprofile` /
  `_zlogin` / `zshlib/` のどこにも無い (repo で pyenv を含むのはこのテストだけ)。
  `command -v pyenv` gate 付きなので手元でも CI (ubuntu) でも**常に skip**。
  同ファイルの Test 4 が同型の false-pass を修正済みと記録しているのに残っている。
  **対応は「テストの削除」が第一候補** (守る対象が repo に無い)
- **`tests/tmux/test_fork_scratch.sh:118` / `:197`** — 検査 A と F が「fork popup / `/fork-scratch`
  は A/B 観測期間で無効」を理由に恒久 skip。無効化は 2026-06 の観測が理由で、その観測は
  もう終わっている (`_tmux.conf` の bind b はコメントアウトのまま)。**復活させるか検査を
  畳むかの判断が要る** (判断が本体で、コードは後)
- **`tests/nvim/test_ftplugins.sh:149-162`** — `]]` の実解決検査が
  `vim.treesitter.highlighter.active[buf]` の有無で無音 skip。parser 未 install 環境で
  flaky を避ける意図は妥当だが、**CI でこの検査が一度も走っていないなら「skip 表示」でなく
  「CI では走らない」ことを可視化**する必要がある (まず CI ログで走行を確認する)
- **`src/parallel-each/result_log_test.go:101-104`** (`TestResultLogCloseIdempotent`) —
  assert が 1 つも無く「panic しないこと」だけ。production の doc が主張する不変条件
  (2 回目の close が無害) を観測していない
- **`src/glogx/open_workspace_test.go:40,:62`** — `want := repoRoot()` と production 関数を
  期待値の出典にする自己言及 (コメント自身が「repoRoot() を使う」と書いている)
- **`tests/_ensure_cli_with_brew.bats:43-44`** — `export -f brew` / `export -f type` は死んだコード
  (bash の関数 export は `zsh -c` の子に継承されず、実際の注入は `declare -f` の文字列展開)。
  🚨 同ファイルの「陽性対照が無い」指摘は**反証で崩れている**ので、この dead export だけが対象
- **`tests/zshrc/lazy-loading/test_version_managers.sh:118-132`** — lazy load の本体
  (`unfunction` → `eval "$(tool init -)"` → 再ディスパッチ) が一度も実行されていない。
  assert は「関数として定義されているか」「`*_ROOT`」「PATH 順」の 3 種だけ。fixture は
  `init -` で `${UPPER}_INIT_CALLED=1` を吐く用意を既にしているのに読む assert が無い。
  **監査時の実測: `_zshrc` の eval 文字列を空関数に置き換えても 12 assert 全部が緑**

## 対応方針

1 件ずつ独立に閉じられる。**各件で「変異を 1 つ当てて red」を確認してから閉じる**
(`mutation-verify-new-tests.md`)。

`072-lazy-body-untested` は **単独で assert を足さない**こと: fixture の heredoc が
unquoted で期待値がずれるため、fixture 修正と同時に着手する (誤った期待値を固定する)。

## trigger

pyenv / fork-scratch の 2 件は「判断」なので単独で着手可 (削除 or 復活の判断がゴール)。
残りは該当テストを次に触るとき。
