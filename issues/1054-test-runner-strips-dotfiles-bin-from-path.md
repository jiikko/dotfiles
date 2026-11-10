# 1054 (test): テストの実行器で、bin にしか無い道具を素の名前で呼ぶテストを手元でも rc=127 で落とす

起票日: 2026-11-10

## 概要

開発機の PATH には `~/dotfiles/bin` が入っている (`_zshrc` の `export PATH="$HOME/dotfiles/bin:$PATH"`) が、CI の PATH には入っていない。
テストが bin の道具を素の名前で呼ぶと、手元では緑なのに CI では rc=127 で落ちる。実例: 84daf0d02 で足した `tests/claude/test_next_claim_push.sh` の検査が `runtimeout` を素の名前で呼び、
Tests run で rc=127 になった (fab386d18 で `"$RUNTIMEOUT"` に直した)。push して CI を待つまで見つからない。

## 対応方針

- **字句の lint (テストの本文を bin の名前で grep する) は採らない**。同じ名前の関数・文字列・引数に誤って当たり、ヘルパーや bin のスクリプトの中から素の名前で呼ぶ間接の形は見えない。
  失敗の仕組み (PATH の違い) そのものを手元に持ち込む方が、検出する範囲が CI と一致する (`adversarial-review-own-safeguards.md` §0)
- **PATH から bin/ を丸ごと外す形も採らない**。`bin/tmux` は本番 tmux サーバを非対話の kill から守る shim で、テストの中の素の `tmux` もこれを通って守られている
  (本番サーバを 2 回殺した経緯は `bin/tmux` 冒頭)。外すとその防御まで消える。`bin/gopls` は自分の dir だけを探索から外すので、symlink を別の dir に置くと自分自身に戻る
- 採った形: `scripts/test_bare_bin_guard.sh` が、`<repo>/bin` の名前のうち dotfiles の bin 以外の PATH に実体が無いものについて、案内を出して rc=127 で落ちる stub を作り、
  その dir を先頭に置いた PATH を返す。Makefile の runner (直列 `run_tests`・並列 `run_tests_parallel`・`test-bats`) が `bare_bin_guard` で作り、`TEST_CHILD_ENV` の `PATH="$$TEST_PATH"` でテストへ渡す。
  別の実体が在る shim (tmux・gopls) は stub にしないので、手元では今までどおり shim に当たる

## 検出しない形・CI と食い違う形 (正本は `scripts/test_bare_bin_guard.sh` の冒頭)

- テストの子が PATH を自分で組み直す / 開発機の PATH の別の場所に同じ名前の別の実体がある道具 / runner を通らない起動 (テストの直接実行・Go のテストの exec・test-pro-con-slow)
- stub は「在るが落ちる」で、CI の「無い」とは違う。`command -v X` で在るかを見る分岐は手元と CI で逆へ進む (在ることだけを assert するテストは手元で緑のまま /
  `command -v X || exit 77` で skip するテストは手元で落ちる)。2026-11-10 時点で bin の道具を `command -v` で見るテストは 0 件 (grep)。敵対的レビューが dev / guard / CI 相当の 3 通りの PATH で関連テストを回し、全部 rc=0

## 進捗

- [x] guard と runner への配線、テスト `tests/scripts/test_test_bare_bin_guard.sh` (判定の単体 + 直列・並列の runner に fixture を流す + test-bats の配線を字面で固定)
- [x] `make test` 全体を配線後に通した: rc=0。guard は 3 つの runner で走った (`[bare-bin] bin にしか無い道具 48 本` × 3)。**既存のテストで素の呼び出しに当たったものは 0 件** (fab386d18 で直した 1 件の後)。skip は配線前と同じ 2 件 (test_claude_links_complete は worktree の判定、test_fork_scratch は macOS で除外)
- [x] 1 回目の `make test` で、runner を入れ子で起こすテスト 3 本 (test_runner_tests_cannot_eat_list / test_test_fail_ledger / test_no_failfast_entrypoints) が落ちた。外側の stub の dir と、偽の HOME の下の本物の `~/dotfiles/bin` を「他の場所に在る」と数えて stub が 0 本になったため。stub の dir の目印 (`.bare-bin-stubs`) と、隣に `_claude/settings.json` を持つ bin を dotfiles の bin に数える判定で直した (guard 自身を目印にしない: push 前の本体・古い checkout が guard を持たない)
- [x] 変異 15 本 (3 回に分けて当てた) がすべて想定どおり red (`bin/mutate-verify-list`): TEST_CHILD_ENV から PATH を外す / 直列・並列・bats の guard の呼び出しを外す / bats の TEST_CHILD_ENV を外す / 目印・`_claude/settings.json`・`-ef $root/bin` の判定を外す / elsewhere を外す / 0 本の検査を外す / symlink の実体解決を外す・`-ef $root/bin/x` に戻す
- [x] opus の敵対的レビュー 3 周。P1・P2 は 0 件。3 周目の採った P3: test-bats の配線の字面の固定が bats を起こす行を名指ししない (`$(TEST_CHILD_ENV) bats "$$t"` で照合する形に直した)。1・2 周目で採った P3: dotfiles の bin を指す symlink が「他の場所」に数えられて stub にならない (1 周目で再現、2 周目で worktree から起こすと再発することを指摘され、実体の dir で判定する形に直した) / `<root>/bin/x` が外の実体を指す symlink を stub にする退行 (同じ直しで解消) / 案内文が PATH を探す shim の経由でも「素の名前で呼んだ」と言う (文面を直した) / test-bats の配線が観測されない (レシピ全体を切り出す字面の固定を足した) / `.` を含む名前の fixture が無い (足した)
- 採らなかったもの:
  - stub が「在るが落ちる」ことによる `command -v` の分岐の食い違い。在るかを見る側を stub に合わせて騙すと、検査が 2 種類の「在る」を区別する必要が出る。現に該当するテストは 0 件なので、ヘッダに書いて受ける。**再提起するなら** `command -v <bin の道具>` で分岐するテストが手元と CI で違う結果になる実例を添える
  - guard が失敗して runner が exit したとき、guard より前に mktemp した一覧ファイル (fails / skips 等) が残る。既存の「mktemp -d 失敗」の経路と同じ形で、今回の変更で増えた形ではない
  - bin のファイルの hardlink を別の dir に置いた形が stub にならない (3 周目の P3)。提案された `-ef "$root/bin/$1"` の併用は、`-ef` が symlink を辿るため、
    外の実体を指す `<root>/bin/x` の symlink を再び stub にする (2 周目で直した退行が戻る。2026-11-10 に `[ /opt/x -ef r/bin/x ]` が真になることを確かめた)。
    開発機に hardlink を置く経路は無いので受ける。**再提起するなら** 開発機の PATH に bin の hardlink が現れる経路を添える
  - 古い checkout (隣に `_claude/settings.json` が無い) の bin を指す symlink が stub にならない (3 周目。`is_dotfiles_bin` の見分け方の限界で、目印を持たない checkout は見分けられない)
