# 1102 (bug): 状態フォルダを渡さずに check を dotfiles の中で動かすと ~/dotfiles/.yomiyasu ができる

起票日: 2026-11-21

## 概要

ja-writing の `check` (と本家の `yomiyasu_check.py`) は、`--state-dir` も `YOMIYASU_STATE_DIR` も無いとき、作業フォルダから上へ辿った git の最上位に `.yomiyasu/` を作って記録を書く。dotfiles の中で試しに動かすと、記録は `~/dotfiles/.yomiyasu/` に入る。
issue 1077 の作業 (2026-11-20〜21) では、この事故が 3 回起きた。

- 1 回目 (2026-11-20 17:34): 実装の係が cwd を `~/dotfiles` にしたまま smoke を動かした
- 2 回目 (2026-11-20 20:21): 別の係が cwd を `tmp/1077-cont-1120/p4a` にしたまま check を動かした
- 3 回目 (2026-11-20 23:52〜11-21 04:02): 4b と 5 本目の係が、本家の行の網羅を測る使い捨ての run を `tmp/1077-cont-1120/p5/cov/run.*` で動かした (runs/ が 139 個)

中身はどれもテストの文の残りで、git には入らない (`.yomiyasu/.gitignore` が `*`)。ユーザーの指示で 2 回とも手で消した。

## なぜ困るか

- 黙って書くので、気づくまで残る (git status に出ない)
- `src/ja-writing/README.md` の書き方どおり、テストと本家の使い捨ての dir は「上に `.git`・`.yomiyasu-words`・`.yomiyasu` があると作らずに止まる」。`~/dotfiles/.yomiyasu` が残っていると、`TMPDIR` を repo の中に向けたテストの止まり方が変わる (未実測)
- 係への指示に「`--state-dir` を必ず渡す」と書いても、網羅の計測や driver の直接の実行で破られた。人が覚える規律では止まらない

## 対応方針 (案。どれにするかは着手のときに決める)

1. dotfiles の `.gitignore` に `.yomiyasu/` を足す。書かれることは止めず、git に入る事故だけを防ぐ (今も中の `.gitignore` が防いでいる)。効き目は小さい
2. ja-writing の oracle と網羅の計測の道具 (`oracle/py/call_function.py` の driver・各係が書いた `cover.py` の写し) が、状態フォルダを渡さずに本家の check を起こしたら止まるようにする。たとえば、起こす前に `YOMIYASU_STATE_DIR` を一時フォルダに向ける
3. dotfiles の中で `ja-writing check` が状態フォルダを決めたとき、そのパスが `~/dotfiles/.yomiyasu` なら警告を出す。本家との互換を崩すので、採るなら dotfiles の wrapper の側に置く

## 関連

- issue 1077 (本文の「事故」の行)
- `src/ja-writing/README.md` の check の節 (状態フォルダの選び方)
