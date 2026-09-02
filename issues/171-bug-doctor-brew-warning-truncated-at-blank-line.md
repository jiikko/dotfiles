# 171 bug: `brew doctor` の警告本文が空行以降で切り捨てられ、修復コマンドと対象一覧が消える

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 2) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「敵対的レビュー 2 回目」の brew doctor P2)

## 対象

`src/glogx/doctor_brew.go` の `parseBrewDoctor`

## 何が起きるか

`parseBrewDoctor` は**空行でブロックを `flush()`** する。`brew doctor` の警告は本文中に空行を含むので、
1 つの警告が複数ブロックに割れ、2 つ目以降は `Warning:` で始まらないため `others` として**黙って捨てられる**。

実測 (実機 Homebrew 6.0.20、rc=1、stderr 2698 byte を parser に通した。実証済み):

| 測ったもの | 値 |
|---|---|
| 警告の数 | 8 |
| 非空行 | 60 |
| parser が残した行 | 33 |
| 捨てられた行 | 27 |

消えるのは実際に人が必要とする部分:

- 「Run `brew link` on these: certifi ruby」(修復コマンド)
- 「Untap them with `brew untap`」(修復コマンド)
- 「kegs have no formulae」の対象一覧

UI の `(N 行)` の数字も過少になり、`Y` のコピー文からも落ちる。

Homebrew 側の `diagnostic.rb` には `\n\n` 入りの heredoc が 11 件あるので、この形は例外ではなく普通に出る。

## 再現手順

```
d=$(mktemp -d); brew doctor > "$d/bd.out" 2> "$d/bd.err"; echo rc=$?
```

`$d/bd.err` (または stdout) を `parseBrewDoctor` に食わせ、残った行数と元の非空行数を比べる。
stdout / stderr / rc を分けて採るのは `measure-external-cli-streams-separately.md` の規律。

## 対応案

- 空行でブロックを切らず、**`Warning:` 行だけで切る** (前置きの 3 行は先に落とす)
- 実機の出力を fixture にしてテストを固定する (行数と、修復コマンド行が残ることを assert)

## 受け入れ条件

- [ ] 実機出力の fixture で、非空行が捨てられないことを確認する
- [ ] 変異検証: 空行 flush に戻すと fixture テストが red になる
