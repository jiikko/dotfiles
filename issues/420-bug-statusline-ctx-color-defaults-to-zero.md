# 420 (bug): statusline のコンテキスト使用率が、取れないと 0% とみなされて緑で出る

起票日: 2026-09-24
反証レビュー: 2026-09-24 実施 (読み取り専用のサブエージェント 1 体)。主要な主張は反証できず

## 概要

`_claude/statusline-command.sh` の冒頭の jq は、同じブロックの他のフィールドを `// ""` (空 = 後段の
`[ -n "$x" ]` で表示を省く) で受けているのに、`context_window.used_percentage` だけ **`// 0`** で受けている。
`total_input_tokens` と `context_window_size` があって `used_percentage` だけ欠けると、ctx の区切りは表示されたまま
色だけ 0% の**緑**になる。「判定できない」を「余裕がある」に見せている。

出典: 2026-09-24 の設計・品質スキャン (観点 5「失敗の握り潰し」)。起票者が再現した。

## 詳細

再現 (起票者が実行):

```sh
echo '{"context_window":{"total_input_tokens":190000,"context_window_size":200000},"workspace":{"current_dir":"/tmp"}}' \
  | bash _claude/statusline-command.sh | cat -v | grep -oE '\^\[\[[0-9;]*m\[ctx:[^]]*\]'
# → ^[[32m[ctx:190k/200k]   (緑。実際は 95%)
# 同じ payload に "used_percentage":95 を足すと ^[[31m (赤)
```

- 抽出: jq のブロックの `(.context_window.used_percentage // 0)`
- 消費: `ctx_part` の組み立てで `rate_color "$ctx_pct"`
- **未確認**: Claude Code が実際に `used_percentage` を欠いた payload を送ってくるか (送ってこないなら実害は無い。
  ただし「欠けたら緑」は壊れ方として最悪の向き)

### 観点 5 の他の結果 (意図的な扱いとして除外したもの)

- statusline の他のフィールドはすべて `// ""` + `[ -n ]` で、失敗は「区切りを出さない」に落ちる (もっともらしい値にならない)
- glogx: `cliUnknown` (判定不能) は通知しない設計 (`src/glogx/cli_health.go` のコメント)。`GHError` は `showWarning` で出る。
  CI の `StateUnknown` は成功に畳まれない。`git status -z` の解釈できないレコードは `skipped` として clean と別に出る
- doctor (disk / docker / brew): `Failed` / `Unavailable` / `Blocked` を 0 や空に畳まない。brew は
  「rc≠0 で警告を読めなかった」を `Clean` にしない
- usage のキャッシュ: 取りこぼし・部分的な snapshot は取り直す
- `UnpushedSHAs` (`src/glogx/gitlog.go`) は `git rev-list` の失敗で nil を返す。コメントに fail-soft の理由がある
  (意図的。記録のみ)
- スキャンの範囲: exec / JSON / キャッシュの境界を中心に見た。tuikit (exec や JSON の境界が無い) と doctor/svc の
  全ファイルは深く見ていない

## 対応方針

- `used_percentage` を `// ""` で受け、空なら `total_input_tokens / context_window_size` から計算するか、
  色を付けずに出す (どちらにするかは実装時に決める)
- `tests/claude/test_statusline.sh` に「`used_percentage` が欠けた payload」のケースを足し、緑にならないことを固定する

## 関連ファイル

- `_claude/statusline-command.sh` (jq のブロック / `ctx_part` / `rate_color`)
- `tests/claude/test_statusline.sh`

## 進捗

- [x] 欠けたときの扱いを決めて直す — 「fix(statusline): ctx 使用率が取れないときに 0% の緑で出さない」
  - 0 に倒していたのは 2 箇所だった: jq の `// 0` と、整数化の後の `ctx_pct=${REPLY:-0}` (`"N/A"` のような
    整数化できない値も 0% になっていた)。どちらも空 (= 分からない) にした
  - 分からないときは区切りを残して**色だけ灰** (`gray_fg`)。トークン数は正しいので区切りは消さない。
    トークン数から割合を計算し直す案は採らなかった: Claude Code の `used_percentage` が何を分母にしているか
    (窓全体か、自動 compact の閾値か) を確かめておらず、別の式で近似すると本物と食い違いうるため
  - `rate_color` に空を渡すと `[ "" -ge 80 ]` がエラーを出して緑に落ちるので、呼ぶ前に分けた
- [x] テストで固定 — `tests/claude/test_statusline.sh` に 5 本 (95% は赤 / 欠けたら灰 / 欠けたら緑にしない /
  `"N/A"` は灰 / 欠けても stderr を出さない)。185 本すべて PASS
  - 変異 (bin/mutate-verify) 3 本とも red: jq を `// 0` に戻す / `${REPLY:-0}` に戻す / 色の分岐を外す
- 敵対的レビュー: **未実施** (2026-09-25 時点で週の利用枠が 99%。変異 3 本で判定の 3 箇所を確認したのみ)
- 未確認のまま: Claude Code が実際に `used_percentage` を欠いた payload を送ってくるか
