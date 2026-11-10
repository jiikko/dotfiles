# 1059 (bug): LANG が空のシェルで、Ruby のスクリプトが日本語を含む JSON を読めずに落ちる (golangci_ruleguard_key.rb ほか)

起票日: 2026-11-10

## 概要

LANG・LC_ALL が空のシェル (Claude Code の Bash。`locale` は LC_CTYPE="C") で、`src/<module>` の `make lint` が
`scripts/golangci_ruleguard_key.rb` の Encoding::InvalidByteSequenceError で落ちる (2026-11-10 に `src/zundamon-kaisetsu` で発生)。
UTF-8 を指定すれば通るので、lint の対象のコードとは無関係に lint が止まる。

## 詳細

### 仕組み (2026-11-10 に実測)

- LANG・LC_ALL が空だと、Ruby の `Encoding.default_external` は US-ASCII になる。`$stdin.read`・`File.read`・`Open3.capture2` の出力は
  どれも US-ASCII の印が付いた文字列になり、日本語のバイトは不正な字になる
- json 2.13.2 (rbenv の Ruby 3.2.2。dotfiles には `.ruby-version` が無く global の版) の `JSON.parse` は、入力を UTF-8 へ `encode` してから読むので、
  そこで Encoding::InvalidByteSequenceError になる。json 2.7.2 (Ruby 3.3.12) は同じ入力で落ちない
- 合成した golangci-lint の設定には、モジュールの `.golangci.local.yml` の日本語の値が入る。日本語を持つモジュールだけが落ちる

### 落ちる所・落ちない所 (dotfiles の下で、LANG・LC_ALL・LC_CTYPE を外して測った)

| 所 | 読み方 | 結果 |
|---|---|---|
| `scripts/golangci_ruleguard_key.rb` (make lint が通す) | `JSON.parse($stdin.read)` | 落ちる |
| `tests/scripts/test_golangci_parallel_runners.sh` の中の `ruby -rjson -e` | `JSON.parse($stdin.read)` | 落ちる (doctor・zundamon-kaisetsu などで ✗。テストは rc=1) |
| `nvim/ruby-refs-index/measure.rb` | rg の出力 (Open3) を 1 行ずつ `JSON.parse` | 落ちる (一致した行に日本語があるとき。同じ読み方を小さく再現して確かめた) |
| `scripts/golangci_config.rb` | `File.read` → `YAML.safe_load` → JSON を書く | 落ちない (YAML の読み込みは通る。`tests/scripts/test_golangci_config.sh` が緑) |
| Makefile の YAML の確かめ (`ruby -ryaml -e "YAML.safe_load(File.read(...))"`) | YAML | 落ちない (上と同じ読み方) |

CI では落ちていない (同じ lint が CI で緑)。CI のシェルが UTF-8 なのか、json が古いのかは確かめていない。

### 測るときの罠

rbenv は `ruby <スクリプトのパス>` では、cwd ではなくスクリプトの置き場所から `.ruby-version` を探す。ubiregi-server の下に置いた
再現のスクリプトは Ruby 3.3.12 / json 2.7.2 で走り、直す前でも落ちなかった (一度「measure.rb は当たらない」と読み違えた)。
再現のスクリプトも dotfiles の下に置いて測る。

## 対応

- `scripts/golangci_ruleguard_key.rb` と `nvim/ruby-refs-index/measure.rb` の冒頭で `Encoding.default_external = Encoding::UTF_8` にする
  (読み込みの全部に効く。読み込みごとに文字コードを付けると、足した読み込みで漏れる)
- `tests/scripts/test_golangci_parallel_runners.sh` の `ruby -rjson -e` に `-E UTF-8` を付ける
- `scripts/golangci_config.rb` は変えない (落ちない)。今の挙動を下のテストで固定する
- テスト: `test_golangci_ruleguard_key.sh` と `test_golangci_config.sh` に、LANG・LC_ALL・LC_CTYPE を外して日本語を含む設定を通す場合を足した
  (CI のシェルは UTF-8 なので、外さないと見えない)
- 入口: `scripts/CLAUDE.md` に Ruby のスクリプトの節を足した (default_external・`-E UTF-8`・rbenv の版の選び方)

### 採らなかったもの

- 呼び出し側 (`scripts/golangci_lint.sh`) で LANG を足す: 直接ほかから起こしたとき (テスト・手で打つ) に効かない
- 全部の Ruby のスクリプトに default_external があるかを字句で見る検査: Ruby の入口は 3 本で、変えたのはそのうち 2 本。振る舞いのテスト (LANG を外す)
  の方が確かで、新しく足すときは `scripts/CLAUDE.md` の節が案内する。Ruby のスクリプトが増えて同じ漏れが起きたら再評価する

## 関連ファイル

- `scripts/golangci_ruleguard_key.rb`・`nvim/ruby-refs-index/measure.rb`
- `tests/scripts/test_golangci_ruleguard_key.sh`・`tests/scripts/test_golangci_config.sh`・`tests/scripts/test_golangci_parallel_runners.sh`
- `scripts/CLAUDE.md` の「Ruby のスクリプト」

## 進捗

- [x] 直した (commit: Ruby のスクリプトが LANG の空のシェルで日本語の JSON を読めずに落ちるのを直す)
- 検証: LANG が空のシェルで `src/zundamon-kaisetsu` の `make lint` が 0 issues。テスト 4 本 (golangci_*) が LANG が空でも UTF-8 でも緑。
  変異 3 本が狙ったテストで red (golangci_ruleguard_key.rb の default_external を外す・golangci_config.rb の読み込みに `encode(UTF_8)` を足す・
  parallel_runners の `-E UTF-8` を外す)。measure.rb はテストが無く、同じ読み方の再現で直す前の rc=1・直した後の rc=0 を見た
