# 変異検証の道具 (mutate・mutate-verify・mutate-verify-list)

テストが主張を本当に守っているかを、実装をわざと壊して (変異) 確かめる道具。判定は 3 つ: RED (変異を検知した) / GREEN (検知できなかった) / 判定不能。
「0 件しか走らなかった」「別の suite が落ちた」「途中で止まった」を RED や GREEN に数えない。詳しい規約・終了コードは各道具の `--help`。

## どれを使うか

| 道具 | 当てる場所 | 向いている場面 |
|---|---|---|
| `mutate` | 今の作業ツリー (当てて、開始時の commit へ戻す) | ビルドの重い app (Xcode のインクリメンタルビルドを使いたい)。未 commit の変更があると拒否する |
| `mutate-verify` | 使い捨ての worktree | どの repo でも。未 commit の変更 (今書いたテスト) も持ち込める。作業ツリーを触らない |
| `mutate-verify-list` | `mutate-verify` を変異ごとに 1 回ずつ | 1 つの変更に変異を何本も当てて、結果を表で見たいとき |

## mutate

repo に呼び口 `bin/mutate` を置く ([calling-from-repos.md](calling-from-repos.md) の形。道具の名前は `mutate`)。`mutate` は `DEVTOOL_RUNTIMEOUT` を使うので、
必ず `devtool-run` 経由で起こす。

```bash
# 未 commit の変更が無い状態で、repo のどこからでも
bin/mutate --timeout 1500 "再生速度の上限を下げる" \
  Sources/Config.swift tmp/<作業名>/mutator.py make test-PlaybackRateTests
```

`tmp/<作業名>/mutator.py` は対象を 1 か所だけ書き換え、当たらなければ rc≠0 で終わる (対象のパスは環境変数 `MUTATE_TARGET`):

```python
import os, sys
p = os.environ["MUTATE_TARGET"]; s = open(p, encoding="utf-8").read()
a = "1.0...5.0"
if s.count(a) != 1: sys.exit("置換の対象が 1 か所でない")
open(p, "w", encoding="utf-8").write(s.replace(a, "1.0...4.0"))
```

- 変異は退行を戻す形にする (期待値を直接埋め込まない)
- RED なら落ちたテストの名前が出るので、狙ったテストかを読む
- 出力 (baseline と変異の run) は run ごとの場所に置き、冒頭の `出力の置き場:` の行に出る

## mutate-verify

dotfiles の `bin/mutate-verify` が呼び口 (dotfiles の `bin/` が PATH にあれば、どの repo からでも `mutate-verify` で呼べる)。
make や CI から呼ぶなら、他の道具と同じく `devtool-run mutate-verify` の呼び口を置く。

```bash
mutate-verify --file src/x/guard.go \
  --apply 'perl -0pi -e "s/if guard/if false \&\& guard/" "$MUTATE_FILE"' \
  --syntax 'go build ./...' \
  --verify 'go test -count=1 -run TestGuard -v ./...' \
  --baseline-expect '^ok ' --expect 'FAIL: TestGuard'
```

- `--baseline-expect` は必須 (runner の集計の行が出たことで「1 件も走っていない」を見分ける)
- `--expect` で「想定した検査が落ちた」ことを確かめる。確かめるのはそこまでで、「それだけが落ちた」ではないので、表示される baseline との差を読む
- `--apply` / `--verify` は eval される。信頼できない文字列を渡さない

## mutate-verify-list

変異の一覧を bash の断片で書き、1 本ずつ `mutate-verify` に渡す。判定は `mutate-verify` のまま。

```bash
# tmp/<作業名>/mutants.sh
defaults --syntax 'go test -vet=off -count=1 -run "^$" ./...' --baseline-expect '^ok '
mutant guard-off --file src/x/guard.go \
  --apply 'perl -0pi -e "s/if guard/if false \&\& guard/" "$MUTATE_FILE"' \
  --verify 'go test -count=1 -run TestGuard -v ./...' --expect 'FAIL: TestGuard/case'
```

```bash
mutate-verify-list --log-dir tmp/<作業名>/mutants tmp/<作業名>/mutants.sh
```

- 一覧は全部読み終えてから当て始める。書き損じがあれば 1 本も当てずに止まる
- ログの置き場は repo の外か、ignore された場所にする (ignore されていないと作業ツリーが変わったと判定される)
