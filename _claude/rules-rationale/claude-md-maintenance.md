# CLAUDE.md の保守ルール — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/claude-md-maintenance.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。

## ルール本文から移した実例 (2026-09-24 の整理で本文から外したもの)

- **共通層を新設する前に X/CLAUDE.md** (2026-08-21 obaket 537): `Storage/HTTP/CLAUDE.md` が「5xx 判定は `HTTP5xxErrorClassifier` に委譲」と明文化していたのに、2 provider の共通部だけ見て 5xx 分岐を再実装した。共通化は既存規約が最も効く場面なのに、規約の参照が最も抜けやすい
- **同じファイル内を grep** (2026-08-28): 同じファイルの 100 行上にある `tt_mtime_of` に気づかず、順序が逆で壊れる版を新設した
- **触るファイルを名指しする open issue** (2026-09-20 obaket 871): `check-polluntil-drift` に 6 群目の比較ブロックを増設したが、issue 831 の冒頭に「今 gate を足すと copy-paste 前提を恒久化するので 829 の着手前に 831 を決着させよ」と明記されていた。この grep は 829 / 831 を両方ヒットさせる
- **深さを前提にした検査** (2026-09-05): `issues/epic/<name>/` を足したとき、番号一意テストだけが旧深さのまま緑を出し続けた
- **手順書は動作で探す** (2026-09-06): issue 291 で group issue の完了先を変えたとき、宣言 5 箇所は揃えたのに issue-sync skill の Step 6 だけが旧契約のまま残った
- **値の契約** (2026-09-21 obaket 888): `screen/move` の directory 分岐を `INVALID_REQUEST` → `accepted` に変えたが、goldenpath skill の step が `INVALID_REQUEST` を assert したまま残り、次のマイルストーンの作業で偶然気づいた
- **コピーでなく単一の正本** (2026-09-19 dotfiles issue 401 / 402): 注記付きのコピー 6 本が正本より古いまま誰にも気づかれていなかった
