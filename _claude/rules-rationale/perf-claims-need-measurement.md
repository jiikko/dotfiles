# 性能を主張するなら、実測値か「未実測である事実 + 実測の trigger」を残す — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/perf-claims-need-measurement.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ (obaket issue 544、2026-08-26)

種別 `perf` の issue で、主題は「preflight が 1 ファイルあたり stat を倍増させる」だった。
実装後に自分が書いた記述は:

- **効果を実測していない**こと (実 SMB 共有が要るため) に触れていなかった
- しかも**効果を過大に書いていた**。preflight を消したことだけ見て、
  **その後に走る canonical stat を数え忘れていた** — 実際は **2 回 → 1 回**であって
  **0 回にはならない**

敵対的な要件照合で指摘されるまで気づかなかった。
**perf の issue で「速くなったはず」を根拠なく書くのは、機能の issue で「動くはず」と書くのと同じ**。

## 母集合を混ぜて「実測」と書いた例 (2026-09-03、dotfiles)

`src/doctor` に lint 設定が無いことを指摘する issue で「production に 12 件」と書いたが、
12 は **production とテストを混ぜた数**だった (正しくは production 10 件 = exhaustive 4 +
未導入 linter 6、テスト込みで 18 件)。`golangci-lint` の出力を `uniq -c` で数えて合計だけを
転記し、母集合を書かなかったのが原因。設定ファイルのコメントにも同じ混同を commit している
(次の commit で production / テストに分けて訂正)。
数字そのものより、**「実測」の見出しの下に母集合の違う数を置いたこと**が問題だった。
出典: `issues/226-retro-glogx-audit-2026-09-03.md` 項目 2 / `issues/222-*.md`。
