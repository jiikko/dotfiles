# 1044 (chore): 予算検査の「他の repo 層」の取り置きが最も重い repo を数えておらず、vlc のセッションが上限を約 13k 超えている

起票日: 2026-11-08

## 概要

予算検査 (`scripts/check_instruction_budget.sh`) は、global 層を 150,000 − `OTHER_REPO_RESERVE` 以内に保つ。
取り置き `OTHER_REPO_RESERVE=39000` は「repo 層が最も重いセッション」の値のはずだが、2026-10-30 の測定は obaket を最大として数えた。
実際には vlc-multi-video-player の repo 層の方が約 14k 重い。計算上、vlc のセッションは今も上限を約 13k 超えている。

## 詳細

### 実測 (2026-11-08)

数え方はスクリプト冒頭と同じ。`paths:` の無い CLAUDE.md と `.claude/rules/**/*.md` を `LC_ALL=en_US.UTF-8 wc -m` で数える。
my-products の app は umbrella の CLAUDE.md と rule も読み込むので、umbrella の 14,988 を足す。

| セッション (起動場所) | repo 層 | umbrella を足した値 | global 層 109,476 を足した値 |
|---|---|---|---|
| apps/vlc-multi-video-player (`15f5c3d4`) | 38,617 | **53,605** | **163,081** (上限を 13,081 超え) |
| apps/dropbox-multi-video-player-electron | 27,687 | 42,675 | 152,151 (2,151 超え) |
| apps/obaket/macOS (`064efead5`) | 19,220 + 5,511 | 39,719 | 149,195 |

- 2026-10-30 の時点でも、vlc の repo 層は 39,950 字あった (同日の最後の commit `8b93d1b1` を同じ数え方で数えた)。
  スクリプトのコメントにある 38,673 (obaket/macOS) は、そのときから最大ではなかった。
- vlc のセッションで実際に上限超えの警告が出ているかは**未確認** (計算だけ)。
- obaket/macOS も、2026-10-30 の 38,673 から 9 日で 1,046 字増え、定数 39,000 を超えた。

### 構造の問題

- スクリプトのコメントと issue 414 は、定数にする理由を「CI からは他の repo を読めない」と書いている。
  測り直しの trigger は「他の repo で上限超えの警告が出たら」で、事後にしか効かない。
- どの repo を数えるかの一覧が無い。手で数えると、最も重い repo を取りこぼす (2026-10-30 に起きた)。
- 他の repo の repo 層は、dotfiles の外の commit で増える。dotfiles の CI はその増加を見ない。
  そのため定数は黙って古くなる (スクリプトの「検出しないもの」に既に書いてある)。

## 対応方針 (案)

CI は他の repo を読めない、という前提は変えない。

1. **取り置きを実測に合わせる (今の手順どおり)**: 53,605 にすると global 層の上限は 96,395 になる。
   global 層 109,476 は 13,081 超えるので、dotfiles の CI が赤くなる。
   上げるなら、どちらの層 (global 層・vlc の repo 層) を縮めるのかを先に決める。
2. **重い repo の側を縮める**: vlc の CLAUDE.md (15,757 字) と rule を claude-md-compact で縮め、repo 層を obaket と同じ程度にする。
   これは vlc の repo の作業で、dotfiles の変更ではない。
3. **測り直しを手から外す**: `gh api` で各 repo の層を取り、最も重いセッションの値を出すスクリプトを作る (clone は要らない)。
   - 数える repo の一覧 (umbrella の submodule・lib・web) を持ち、app には umbrella の層を足す。
   - gh の認証がある手元の入口 (`make test-lint` の手元の実行・pre-push) でだけ走らせ、定数より大きければ落とす。
     CI では今と同じく定数だけを見る (gh が使えないことを出して飛ばす)。
4. **起点を他の repo の側へ**: 重い repo に「自分の repo 層 + global 層 > 150,000」で落ちる検査を置く。
   - 手元では global 層を `~/.claude/` の実体から数えられるが、dotfiles の無い環境 (CI) では数えられない。
   - CI で回すなら global 層の値を定数で持つことになり、今の問題の向きを変えただけになる。

- 2 で vlc を縮めれば 1 の値を下げられる。3 と 4 は重ねて入れてもよい。
- どれを入れるかはユーザーに決めてもらう。

## 関連ファイル

- `scripts/check_instruction_budget.sh` (冒頭のコメント・`OTHER_REPO_RESERVE`)
- `Makefile` の `test-instruction-budget` (`make test-lint` と CI の Lint で走る)
- [issue 414](done/414-docs-scope-global-rules-with-paths-under-150k.md) (上限の経緯と再開の trigger)
- my-products の `apps/vlc-multi-video-player/CLAUDE.md`

## 進捗

- [x] 反証レビュー (codex は使用上限のため、読み取りのみのサブエージェント 1 体で代替)。
  指摘 P1「最も重いのは obaket ではなく vlc」を実測で確かめて本文を書き直した。
  P3 (方針 4 は global 層の定数が逆向きにずれる・数える repo の一覧が要る) も反映した。
- [ ] 方針を決める (1〜4 のどれを入れるか)
