# 463 (bug): `pro-con card run` が argv を空白で繋いで eval するので、引用が壊れて別のコマンドになる

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

もろい作りの監査 (460) の観点 2。PG がテストの係に頼むコマンドが、引用を含むと黙って別物になる。

## 詳細

- 該当: `src/pro-con/cardcmd.go` の `parseCardArgs` (run: `strings.Join(rest[2:], " ")`) と、テストの係がそれをシェルの 1 行として eval する所 (`runScript`)
- 発火条件: PG が引用つきで頼む。例 `pro-con card run C-001 -- go test -run 'A|B' ./...` / `-- sh -c 'x; y'`
- 壊れ方: シェルが PG のシェルで一度引用を外した argv を空白で繋ぐので、`A|B` はパイプになり rc=127。`sh -c 'echo a; touch "x y"'` は `sh -c echo` の後に
  `touch` を外側で走らせて rc=0。**rc=0 のまま別のテストを走らせた結果が PG に届きうる**
- 根拠: 同じ形 (Join + `bash -c` の eval) を bash で再現 (監査の係)。`TestCardRunParse` は引用の無い形だけを固定している。2026-09-25 の実際の run は 10 件すべて `make test`

## 対応方針 (候補)

- argv を 1 つずつ quote してから繋ぐ (Go で POSIX シェルの quote を書く)。または argv の配列のまま記録に持ち、実行も配列で渡す (シェルを通すのをやめる)
- どちらにするかは、PG がパイプや `&&` を含む 1 行を頼みたい場面 (`-- 'make test 2>&1 | tail'` のような 1 引数の形) を残すかで決める。README と pm-guide の書き方も合わせる

## 対応 (C-018)

- 決めたこと: **argv を 1 つずつ POSIX シェルの quote にしてから繋ぐ** (`src/pro-con/e2ecmd.go` の `shellJoin`。記録はシェルの 1 行のまま、runner の eval も変えない)。
  引用の要らない語はそのまま残すので、記録・画面の `make test` は今までどおり
- 1 引数の 1 行 (`-- 'make test 2>&1 | tail'`) は残さない: 1 語として quote されるので rc=127 で**目に見えて**落ちる (黙って別物にはならない)。
  パイプや `&&` を含む 1 行は `-- bash -c '<1 行>'` で頼む、と README と PG への指示文 (`dispatcher.go`) に書いた
- 検査: `TestCardRunParse` に、引用つきの argv (`A|B` / 空白 / `'` / 空文字 / `$HOME` / `*` / `;` / `\` / 改行) を bash で eval して同じ argv へ戻るかを足した。直す前は red (exit status 2)
- [ ] 変異 2 本で red (shellJoin を素の Join / `'` のエスケープを外す)。bin/mutate-verify を card run で
- [ ] make test

## 関連

- 460 (監査の記録)
