# 820 (bug): worktree を消した後、到達不能の検査が消えた worktree のパスで赤くなる

起票日: 2026-10-18

## 概要

`scripts/check_unused_excluding_tests.sh` (到達不能の検査) が、**既に消した worktree のパス**を含む結果を出して赤くなることがある (retro 819 の 4)。
この repo は作業ごとに worktree を切って消す運用 (`.claude/rules/worktree-per-session.md`) なので、毎回の作業で踏みうる。

## 詳細 (2026-10-18 の観測)

- `dotfiles-wt-817` を消した後、新しく切った `dotfiles-wt-817b` で検査を回すと、次の 2 件で rc=1 になった:
  - `✗ production から到達できない: src/schedkeys/../../../dotfiles-wt-817/src/schedkeys/editor.go:(*editor).setValue`
  - `✗ allowlist が stale: src/schedkeys/editor.go:(*editor).setValue は staticcheck から報告されていない`
  (報告のパスが消した worktree を指し、allowlist のキーと一致しないので、同じ 1 件が「到達不能」と「stale」の両方に出る)
- 同じ木で `STATICCHECK_CACHE=<空の dir>` を付けると緑になった。後の `-817c` でも、`-817b` を指す同じ形が出た
- スクリプトは staticcheck をキャッシュの指定なしで呼ぶ (`cd "$m" && staticcheck -checks=U1000 -tests=false -tags=nofixture ./...`)。
  既定のキャッシュ (`~/Library/Caches/staticcheck`) が worktree をまたいで共有されている
- **再現した (2026-10-18)**: 一時 worktree `dotfiles-wt-820x` で検査を回し (キャッシュを作る)、消してから別の worktree で回すと、
  `src/glogx/../../../dotfiles-wt-820x/src/glogx/zoom.go:(*appZoom).start` が「到達できない」と「stale」の両方に出て rc=1。
  同じ木で `STATICCHECK_CACHE=<空の dir>` にすると rc=0。キャッシュが前の worktree の絶対パスを持ったまま再利用されている
  (staticcheck の中で何をキーにしているかまでは読んでいない)。allowlist に載っている 2 件 (zoom.go / editor.go) が毎回この形で出る

## 測ること (着手したら最初に)

1. (済み。上の「再現した」) worktree をまたいだ再利用で赤くなる
2. (済み) `STATICCHECK_CACHE` を分けると緑
3. (済み。下の「結果」) 同じ worktree で 2 回続けて回す所要を、キャッシュ共有のときと分けたときで比べる

## 対応方針 (測ってから決める)

- 候補: スクリプトが `STATICCHECK_CACHE` を checkout ごとの置き場 (`git rev-parse --show-toplevel` から決める) に向ける。
  worktree の中で終わる置き場は、worktree と一緒に消える場所にする (`tmp/` の規約は `~/.claude/CLAUDE.md`「一時ファイルの配置」)
- 却下: スクリプトが出力のパスを正規化して比べる (staticcheck が古いキャッシュを返していること自体を隠す。消したファイルの結果を信じることになる)

## 関連ファイル

- `scripts/check_unused_excluding_tests.sh`
- 観測の出典: issue 819 の 4

## 反証レビュー

省略した。中心の主張 (worktree をまたいだキャッシュの再利用で赤くなる / キャッシュを分けると緑) は起票の前に実測で再現したため。対応方針はまだ反証されていない

## 結果 (2026-10-18)

- 測定 (この Mac。staticcheck v0.7.0 を入れて測った。22 モジュール):
  - 別のマシンの観測と同じ形を再現した: 一時 worktree で回して消した後、別の worktree で回すと allowlist の 2 件が「到達できない」と「stale」の両方に出て rc=1
  - GOFLAGS の `-trimpath` (issue 816) は原因ではない: `GOFLAGS=` にしても同じ rc=1
  - 既定のキャッシュのまま、その worktree で 2 回回し直しても rc=1 のまま (古い結果が当たり続ける)
  - 所要: キャッシュを分けた 1 回目 (冷) 12.3 秒 / 2 回目 (温) 8.3 秒、共有のキャッシュ (温) 8.4 秒。差は新しい checkout の最初の 1 回だけ
- 直したこと (`scripts/check_unused_excluding_tests.sh`): `STATICCHECK_CACHE` を `<git dir>/staticcheck-cache/<checkout の場所のハッシュ>` にする
  (呼び出し側が渡したらそれを使う)。git dir は `git worktree remove` で一緒に消える (実測)。場所でも分けるのは、`git worktree move` や
  本体の mv では git dir が変わらず、前の場所のパスで返るから。git を呼ぶときは継承した `GIT_DIR` / `GIT_WORK_TREE` を外す
- 確認: 修正後の木で「worktree で回して消す → 別の worktree で回す」と「worktree で回す → `git worktree move` → 回す」がどちらも rc=0
- 回帰テスト `tests/scripts/test_unused_check_cache_per_checkout.sh` (偽の staticcheck で渡った置き場を見る。期待値は `.git` を直接読んで出す)。
  変異: 場所ごとに分けない / GIT_DIR を外さない / export しない の 3 本で red
- 敵対的レビュー (opus 1 体): 採用 2 件 = git dir だけで分けると move の後に古い結果が当たる (場所のハッシュで分けた)・継承した GIT_DIR で
  置き場がずれる (外した)。テストの期待値を実装と同じ式で出していた点も直した。記録のみ: git の外 (git archive の木) で回すと rc=1 で止まる
  (CI の checkout は git なので影響なし)・本体の `.git/staticcheck-cache` は消えないので増えうる (staticcheck が自分で刈り込むかは未確認)。
  壊せなかった: 同じ checkout の中の rename・同じパスへの作り直し・`.git` がファイルの形・書き込めない git dir (赤に倒れる)・CI (unused.yml は
  actions/cache を使わず毎回冷えた状態)

