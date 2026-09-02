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

## 対応 (2026-09-03)

**修正した。** `src/glogx/doctor_brew.go` の `parseBrewDoctor` で、ブロックの切れ目を **`Warning:` 行だけ**にした。

- 空行では `flush()` しない。ただし**段落の切れ目としては 1 行残す** (敵対レビュー 2 周目の指摘。
  前置き allowlist に無い行が警告のあいだに現れたとき、直前の警告の本文へ地続きに見えるのを避ける)
- 連続する空行は 1 つに畳む。塊の先頭・末尾に空行は持たない (`flush()` で末尾をトリム)
- `doctor_view.go` の `(N 行)` を `len(lines)` から **`len(detail)`** に変更。これは「Enter で何行出てくるか」の
  予告なので、見出しは数えず段落の空行は数えるのが正しい

fixture は実機 (Homebrew 6.0.20 / rc=1 / stderr) を `doctor_brew_test.go` に入れた。
`Run \`brew link\` on these:` と対象一覧 `  ruby` が残ることを assert している。

### 変異検証

| 変異 | 結果 |
|---|---|
| 空行で `flush()` する旧挙動へ戻す | red (`want=9 got=7`、落ちた行に修復コマンドと対象一覧) |
| `Warning:` の `flush()` を消す (全部 1 塊) | red |
| 段落の空行を捨てる | red |
| `flush()` の末尾トリムを外す | red |
| 連続空行の畳み込みを外す | red |
| `(N 行)` を `len(lines)` に戻す | red (`(7 行)`) |
| `(N 行)` を非空行の数に戻す | red (`(5 行)`) |

⚠️ 最後の 2 つは、既存 fixture (空行なし) では 3 つの数え方が同じ数になり **区別できなかった** (vacuous)。
`TestDoctorLinesFillsPage` の fixture に段落を 2 箇所入れて、`len(detail)=6` / 非空行`=5` / `len(lines)=7` が
全部違う数になる形にしてから変異を当てた。

### 敵対的レビュー (sonnet / read-only / 2 周)

1 周目 5 観点: 採用 1 / 却下 0。

- **採用**: 前置き allowlist に無い行が警告のあいだに現れると、直前の Warning の本文へ地続きに混入する
  (再現済み)。修正前は「黙って捨てられる」だったので情報消失は改善しているが、「1 見出し = 1 塊」の
  不変条件が崩れる。→ 段落の空行を残す形にした
- **壊せなかった**: rc=0 / `Error:` のみ / 前置きだけ / stdout に警告 + stderr が前置きだけ の各経路、
  既存 `TestParseBrewDoctor` の意味、UI の消費側

2 周目 5 観点: 採用 2 / 却下 0。

- **採用**: `(N 行)` が実際の展開行数と系統的にずれる (段落 0 個で 1 多く、2 個以上で少なく出る)。
  しかも既存 UI テストの fixture に空行が無く、この修正を守るテストが 1 本も無かった → 上のとおり修正
- **採用**: 連続する空行が畳まれず増殖する → `cur[len(cur)-1] != ""` のガードを足した
- **壊せなかった**: 末尾トリムが意味のある行 (`  ruby`) を削る / 空白だけの行 / 塊が空行だけになるケース /
  rc=0・`Error:`・前置きだけの各経路

### 却下・受容した指摘

- 「空行 case のガード (`len(cur) > 0`) を外す変異を、新規テスト 2 本は検出できない (検出したのは既存の
  `TestParseBrewDoctor` の `Error:` ケースだった)」 → **受容**。不変条件 (塊の先頭に空行を持たない) は
  既存テストが守っており、二重に固定する価値が薄い。今回「見出しだけの塊」のケースを新規テストに足したので、
  先頭の空行が残ると `Warnings[0] != "Warning: A"` で red になる形にはなっている
