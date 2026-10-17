# 820 (bug): worktree を消した後、到達不能の検査が消えた worktree のパスで赤くなる

> 🚨 **担当中: air5 (glogx)**（2026-10-18〜）

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
3. 同じ worktree で 2 回続けて回す所要を、キャッシュ共有のときと分けたときで比べる (分けるとキャッシュの効きがどれだけ落ちるか)

## 対応方針 (測ってから決める)

- 候補: スクリプトが `STATICCHECK_CACHE` を checkout ごとの置き場 (`git rev-parse --show-toplevel` から決める) に向ける。
  worktree の中で終わる置き場は、worktree と一緒に消える場所にする (`tmp/` の規約は `~/.claude/CLAUDE.md`「一時ファイルの配置」)
- 却下: スクリプトが出力のパスを正規化して比べる (staticcheck が古いキャッシュを返していること自体を隠す。消したファイルの結果を信じることになる)

## 関連ファイル

- `scripts/check_unused_excluding_tests.sh`
- 観測の出典: issue 819 の 4

## 反証レビュー

省略した。中心の主張 (worktree をまたいだキャッシュの再利用で赤くなる / キャッシュを分けると緑) は起票の前に実測で再現したため。対応方針はまだ反証されていない
