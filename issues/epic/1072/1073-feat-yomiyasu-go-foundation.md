# 1073 (feat): yomiyasu の Go 実装の土台 (module・Python の re を写す層・本家の出力を正解にする検査)

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-12〜）

起票日: 2026-11-12

## 概要

1074〜1078 が共通に使う土台を作る。中身は 4 つ。

- `src/ja-writing` の module と CLI の入口、package どうしの公開面
- Python の標準ライブラリの振る舞い (`re`・`json`・`str`・`unicodedata`) を Go で写す内部の package
- 本家の Python の実装を正解 (oracle) として出力を記録し、Go の実装と突き合わせる仕組み
- 本家と揃わないことを認める範囲の表

## 測った事実 (2026-11-12、本家 v3.1.0 = commit db78b9bc)

- Python のコードは全部で 25,987 行 (`wc -l`)。内訳は markdown_visibility 1,429・lint 6,398・diff 11,577・check 5,639・hook 944
- 読み込みの依存は markdown_visibility ← lint ← diff ← check ← hook の順。diff は絵文字の数え方 `_emoji_matches` を lint から借り、読めなければ簡単な数え方にする
- 正規表現の後読み (`(?<=` `(?<!`) は 443 か所、先読み (`(?=` `(?!`) は 390 か所。Go の標準の `regexp` (RE2) はどちらも書けない。
  repo は既に [regexp2](https://github.com/dlclark/regexp2) の `v2 v2.5.2` を間接の依存に持つ (glogx・tuikit・treefiler・pro-con)。
  これを直接の依存にする。手元の試験は v1.12.0 で行ったので、v2 で同じ試験を回し直す
- regexp2 の試験の結果 (`tmp/yomiyasu-gap-2026-11-12/probe-regexp2`)
  - 先読み・後読みは通る。位置は rune 単位で返り、Python の str の添字と同じ単位になる
  - `(?P<名前>` は構文エラーになる
  - 「空の一致の直後は、同じ位置で空の一致を返さない」(Python 3.7 以降) は、`(?:パターン)(?!\G)` で次の一致を探すと再現できた。
    7 つの入力 (`a*|b` に `b`・`x*` に `abxd` など) で Python 3.15.0 の `re.findall` と同じ結果になった
- 全コードポイントで比べた字の集合の差 (Python 3.15.0 と regexp2 v1.12.0)
  - `\w`: Python だけが当たる字が 10,812 (No・Nl と、Unicode 16/17 で増えた字)。regexp2 だけが当たる字が 1,996 (Mn・Pc・Cf・Mc)
  - `\s`: Python だけが `\x1c`〜`\x1f` に当たる
  - `\d`: Python だけが当たる字が 90 (Unicode 16/17 で増えた数字)
- Python 3.15.0 の `unicodedata` は Unicode 17.0.0、Go 1.26 の `unicode` と `golang.org/x/text` の norm は 15.0.0。1 字ずつの NFKC の結果は 37 字で違い、NFC は 0 字だった。
  Python と Go の Unicode のバージョンの差が出るのは、`unicodedata.category` (8 か所) と `normalize` (8 か所)、それと上の字の集合
- .NET の構文は名前付きのグループを名前の無いグループの後に番号付けする。Python は開き括弧の順に番号を振る
- 本家が使う `re` の機能
  - `re.escape` (27 か所)・`lastgroup` (5)・`lastindex` (2)・`groupdict` (2)・`.flags` / `.groups` / `.pattern` の読み出し
  - `pos` / `endpos` 付きの search (lint と diff に 5 か所)
  - フラグの `re.M` (8)・`re.I` (9)・`re.UNICODE` (1)、行内のフラグの `(?m)` (5) と `(?i)` (1)
  - 置換の書式 `\1` (1)。`\g<名前>` と `(?P=名前)` は使っていない
  - 所有格の量指定子・アトミックグループ・`re.X`・`re.A` は使っていない
- JSON の書き方
  - lint は `indent=2`、diff は `indent=1`
  - check は `indent=1`・`sort_keys` と `separators=(",", ":")` の組・指定なしの 3 通り
  - hook は指定なし (区切りは `", "` と `": "`)・`indent=2`
  - どれも `ensure_ascii=True`。読む側の `json.loads` は hook と check に 4 か所ある
- lint の絵文字の表は Unicode 18.0 の emoji-data などから作ったもので、Unicode License V3 の表示が要る (lint の冒頭の注記)

## 対応方針

### 置き場所・CLI・公開面

- `src/ja-writing` (module `ja-writing`)。CLI は 1 本の `ja-writing` で、subcommand に `lint` / `diff` / `check` / `hook` を持つ。
  各 subcommand のオプションは本家の argparse の定義と同じにする
- 各 subcommand を `Main(ctx, env) int` の形の関数にする。`env` は引数・標準入出力・cwd・環境変数を持つ。
  check (1077) が lint と diff を子プロセスで動かすか関数で呼ぶかを後で決めても、公開面が変わらないようにする。
  diff が借りる絵文字の数え方は lint の package の公開の関数にする
- 入口は `bin/ja-writing` (`go_autobuild_exec`。zsh なら root Makefile の `ZSH_SYNTAX_FILES` に登録する)
- `src/README.md` の 3 点セット (Makefile の `lint` / `test`・`go.mod` と `go.sum`・`.github/workflows/src_ja-writing.yml`) と、package の doc comment を揃える。
  `src/README.md` の一覧と `src/PACKAGES.md` (生成物) に足す
- ライセンス: 本家の `LICENSE` (MIT) と `UNICODE-LICENSE.txt` を `src/ja-writing/` に置く。`_claude/skills/ja-writing/textlint/LICENSE-yomiyasu` は
  別の配布物 (textlint の規則) に付くので残し、module には複製を置く
- 語の一覧 (`references/lists/*.txt` の 8 本) は lint・diff・check が共通に読むので、ここで `go:embed` で埋め込む。
  中身と SHA-256 を本家と同じにし、`.gitattributes` で改行の変換を止める (`-text`)。check の記録はこの SHA-256 を入れる

### Python の標準ライブラリを写す内部の package

- `internal/pyre`: Python の `re` のパターンを regexp2 の構文に直して compile する
  - 名前付きのグループは名前の無いグループに直し、名前と番号の対応を自分で持つ (Python の番号の振り方を保つ)
  - `\Z` は `\z` に直す。`\s` `\S` `\w` `\W` `\b` `\B` `\d` `\D` は、Python の定義の字の集合で置き換える
  - 行内のフラグ、フラグの `re.I` / `re.M` / `re.UNICODE` を受ける
  - 後読みが固定幅でないパターンは、Python と同じくエラーにする (.NET は可変幅を受けるので、断らないと本家より多く当たる)
  - API は `search` / `match` / `fullmatch` (`pos` / `endpos` つき)・`finditer` / `findall` / `sub` / `subn` / `split`・`escape`、
    一致の `groups` / `groupdict` / `lastgroup` / `lastindex`、パターンの `flags` / `groups` / `pattern`。置換の書式は Python のもの
  - 照合の時間の上限は付けない (本家に無い)。止まらない照合の危険は 1074 と 1075 で最悪の入力を測って扱う
- `internal/pyunicode`: Python の `unicodedata` (17.0.0) から生成した表。全コードポイントの一般カテゴリと、Go の norm (15.0.0) と結果が違う字の NFKC / NFC。
  生成の道具は oracle と一緒に置き、生成した表を commit する
- `internal/pyjson`: `json.dumps` と同じ字を出す。本家が使う組み合わせを全部持ち、`ensure_ascii`・小数の `repr`・`round` の値・挿入順の鍵も揃える。
  `json.loads` も、本家が読む 4 か所の入力で同じ受け方・断り方をする
- `internal/pystr`: Python の str の振る舞い (`splitlines` が区切る字・`isspace` / `isalnum` などの分類・`strip` の空白)

### 本家の出力を正解にする仕組み

- `src/ja-writing/oracle/` は本家のファイルを固定の commit (db78b9bc) から取ってくる。git clone はしない
  - 取ってきたファイルを sha256 の一覧で照合し、`${XDG_CACHE_HOME:-~/.cache}/yomiyasu-oracle/<commit>/` に置く
  - 取ってきたファイルには手を入れない
- 本家を動かす環境を固定する
  - `env -i` から始め、`PATH`・使い捨ての `HOME`・`TZ=UTC`・`PYTHONHASHSEED=0` だけを渡す
  - cwd は使い捨ての dir にする
  - 継承した `GIT_DIR` などを外す (`bin/lib/git_location_env.sh`)
  - Python は `python3 -I -B -X utf8` で動かす
- 記録は 3 つの単位で取り、`testdata/golden/` に置く。各記録には本家の commit・Python のバージョン・`unicodedata` のバージョンを入れる
  1. **パターン**: 本家の道具を入力の集まりに通す間、`re` の compile を包んで、使われた全パターン (パターンとフラグ) を記録する。
     実行時に組み立てるパターンも含む。記録したパターンを入力の行に当て、一致の位置・長さ・グループを残す。
     Go のテストは同じパターンを `internal/pyre` で compile して比べる。lint・diff の Go の実装が無くても、この issue の中で確かめられる
  2. **関数**: 本家の module を import して関数に JSON の引数を渡し、戻り値を JSON で残す driver。subcommand が無い段階 (1074) でも比べられるようにする
  3. **CLI**: subcommand・引数・入力の組ごとの stdout・stderr・rc
- `make golden` (手で打つ) で記録を作り直す
  - 記録の Python と Unicode のバージョンが今の環境と違うときは止まる (`UPDATE=1` を付けたときだけ上書きする)
  - 2 回作って同じ記録になることを確かめる
  - 作り直した記録の差分は、commit の前に読む
- Go のテストは記録と突き合わせるだけで、CI でも Python を動かさない。Python が無いときにテストを飛ばして緑にする経路は作らない
- 入力の集まり (`testdata/corpus/`) の元
  - 本家の references の例文と、本家のテストの入力
  - dotfiles の文書
  - 手で作った端の入力 (CRLF・NFD・`\x1c`〜`\x1f`・絵文字・BMP の外の字・Unicode 16/17 で増えた字・空のファイル)
- 空振りを止める検査
  - 記録から規則 ID ごとに「当たった入力」「当たらなかった入力」の件数を数え、当たる入力が 1 件も無い規則があれば落とす
  - 入力の集まりが 0 件・記録が無い・入力と記録の件数が合わないときも落とす

### 本家と揃わないことを認める範囲 (この表に無い差は全部、不一致として落とす)

| 範囲 | 扱い |
|---|---|
| argparse の usage とエラーの文面 | rc だけを揃え、文面は比べない |
| 本家のコマンド名 (`yomiyasu_lint.py` など) を含む案内の文 | `ja-writing lint` などに置き換えてから比べる |
| check の記録の時刻・ホスト・パス・`python` の欄 | 置き換えの印にしてから比べる (欄の扱いの中身は 1077 で決める) |
| `--version` の文字列 | 本家と同じ `3.1.0` を出す (Go の実装であることは別の欄で出すかを 1078 で決める) |

## 受け入れ条件

- [ ] `make -C src/ja-writing lint test`・root の `make test-lint`・`make test-dupscan-gate` が通り、CI の `src/ja-writing` が緑
- [ ] `internal/pyre` が、パターンの記録の全パターン・全入力の行で Python の `re` と同じ一致を返す
- [ ] `\w` `\s` `\d` `\b` と `re.I` の 1 字の照合が、全コードポイントで Python と同じになる (生成した表で試す)
- [ ] `internal/pyunicode` の一般カテゴリと NFKC / NFC が、全コードポイントで Python 3.15.0 (Unicode 17.0.0) と同じになる
- [ ] `internal/pyjson` が本家の使う全部の組み合わせで `json.dumps` と同じ字を出し、`json.loads` の受け方・断り方も揃う
- [ ] `make golden` が環境を固定して本家を動かし、2 回作って同じ記録になる。取ってきたファイルの sha256 が合わなければ止まる
- [ ] 揃わないことを認める範囲の表が `src/ja-writing/README.md` にある
- [ ] 実装をわざと壊して落ちることを確かめた記録がある (`mutate-verify`)。最低限は次の 3 つ
  - `\s` の直しを外す
  - 空の一致の規則を外す
  - 入力の集まりを空にする
- [ ] 新しく作った検査の仕組み (oracle・記録・突き合わせ) を、敵対的レビュー (opus) に通した

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/*.py`・`skills/yomiyasu/references/lists/*.txt`
- `src/README.md` (新しいプロジェクトの決まり)・`bin/lib/go_autobuild.zsh`・`bin/lib/git_location_env.sh`

## 反証レビュー (2026-11-12、事実の誤り・計画の穴の 2 観点を 1 体ずつ)

指摘は 30 件 (事実 14・計画 16)。全部を採り、epic 1072 の各 issue に反映した。この issue に入れたものは次のとおり。

- regexp2 は repo の依存に合わせて v2 にする
- `re` の API・グループの番号・可変幅の後読みの列挙を足した
- JSON の組み合わせを全部挙げ、`json.loads` も入れた
- パターンの記録を本家の実行から取る形にし、1073 の中で確かめられるようにした
- 関数の単位の driver を足した
- 環境の固定・記録の再現性・空振りを止める検査・揃わない範囲の表を受け入れ条件に入れた
- 語の一覧 8 本とライセンス (Unicode License V3) の扱いを足した

採らなかった指摘は無い。

## 進捗

### 1 周目の実装 (2026-11-13、係 3 体を 1 体ずつ。commit は下の 4 本。仮の名前 src/yomiyasu で作った)

- 骨組みと oracle: `feat(yomiyasu): Go 実装の骨組みと、本家 (Python) の出力を正解にする oracle を作る`・`fix(yomiyasu): 200 以外の応答でも本文を閉じ、lint と dupscan の指摘を直す`
  - 入力の集まり 331 ファイル (本家の tests/corpus 187・本家のテストの文字列 114・references 9・dotfiles 7・端の入力 14)
  - 本家が使った (パターン, フラグ) は 839。そのうち 765 が入力のどこかに当たった。本家の道具を 674 回動かし、全部 rc=0
  - Python の起動は `-I` でなく `-P -s -B -X utf8` にした。`-I` は `PYTHONHASHSEED` を含む `PYTHON*` を全部無視し、set の反復順に依存する出力が実行ごとに変わりうるため (手元の 3.15.0 で `hash('a')` が実行ごとに違うことを確かめた)
- pyunicode・pystr・pyjson: `feat(yomiyasu): pyunicode・pystr・pyjson と、Python から作る記録・表`
  - pyunicode は Go の unicode と x/text の norm を使わず、Python から生成した完全な表と正規化の算法を持つ (Unicode 16/17 で増えた合成を文字列の中で取りこぼさないため)
  - splitlines が区切るのは `\x1c`〜`\x1e` で、`\x1f` は入らない (`\s` は `\x1c`〜`\x1f`)
- pyre: `feat(yomiyasu): internal/pyre を作る (Python の re を regexp2 v2 で写す層)`
  - 構文木への読み込みは CPython の `re._parser` の写し。`\s \w \d \b` と re.I は Python の表を字の範囲に開く
  - regexp2 の後戻りの誤り: 幅の無い選択肢 (`(?:^|;)` など) の後に「1 字以外」の集合が来ると当たりを落とす。補集合を 0xFFFF で 2 つに割って避けた (`TestZeroWidthAlternativeBeforeComplementSet`)
- 検査: module の lint・test (race つき。パターンの突き合わせは race 無しの 1 本)・`make test-dupscan-gate`・`scripts/go_packages_index.sh --check` が通った。
  実装をわざと壊して落ちることを `mutate-verify-list` で 52 本確かめた (係ごとに 8・35・9)
- 記録の大きさ: golden 4.3 MB・入力の集まり 2.4 MB・生成した表 `tables_gen.go` 363 KB

### 敵対的レビュー 1 周目 (opus、2026-11-13)

P1 (本家と違う答えを緑のまま通す) は 0 件、P2 は 5 件、P3 は 7 項目。どれも再現の手順つき。

| 指摘 | 扱い |
|---|---|
| P2-1 BMP の外の字 1 つの否定 (`x*[^😀]` を `x_` に当てると Python (0,2)・Go (1,2)) | 直す: 補集合を割る条件が `last.lo <= 0xFFFF` に限られていた。→ 直した (2 周目): 集合が BMP の内と外にまたがるなら 0xFFFF で割る (`emitSet`)。`x*[^😀]` ほか 4 形を `TestZeroWidthAlternativeBeforeComplementSet` と記録に足した |
| P2-2 行内の `(?a:...)` の中の `\W \S \D \B` と否定クラス | 直す。→ 直した: 本家は先頭が集合のパターンの search の開始位置を全体のフラグで絞る (`_compile_info`)。`searchFilter` で写し、`_parse_sub` の分岐のまとめ方 (`mergeBranch`) も写した。記録に `(?a:...)` 23 形 × 9 入力 × 2 フラグを足した |
| P2-3 空になれる本体の `{m,n}` (`(a*?){1,2}` の group 1) | 直せるか試す。直せなければ揃わない範囲として README に書く (本家の 839 パターンには無い形)。→ 直した: `{m,n}` を `{m}` と `{0,n-m}` に割って書く (`unrollRepeat`。同じ番号の捕獲グループを `(?<N>` で 2 か所に書く)。記録に 24 形を足し、揃った |
| P2-4 oracle の sandbox が TMPDIR の上の repo の `.git` と `.yomiyasu-words/` を拾い、記録が汚れる | 直す。→ 直した: 使い捨ての dir の上に `.git` `.yomiyasu-words` `.yomiyasu` があれば作らず止まる (`TestNewSandboxRefusesDirUnderUpstreamMarker`) |
| P2-5 pyjson で `"\u0041` のように `\u` の直後で終わる文字列のエラーの種類と位置 | 直す。→ 直した: `end > n` に直し (Python の文面と位置に揃った)、記録の loads に 16 形を足した |
| P3 `pos > endpos` のときの空の一致 | 直す。→ 直した: `match` だけ字を読まない空の一致を返す (`formPastEnd`。末尾の判定・1 字の繰り返し・後読みは満たせない扱い)。記録に 12 形。`(?m)$` は本家が範囲外の字を読むため写せない (README) |
| P3 グループ名の isidentifier の近似 (26 字・36 字の差) | 直す (Python から表を作る)。→ 直した: `identifier_start` / `identifier_continue` を全コードポイントで記録し、表を生成した (`IsIdentStart` / `IsIdentContinue`) |
| P3 自分のグループを参照する条件付きグループ | 直せるか試す。直せなければ README に書く。→ 揃えられず README に書いた: regexp2 に位置の比較が無い。本家のパターンに `(?(` が無いことは `TestPatternsMatchPythonRecording` が見る |
| P3 本家のソースにあるのに 674 回の実行で compile されなかったパターン 18 個 | 直す: 記録に入れる。→ 直した: ソースを ast で読み literal 95 個を集め、使われなかった 18 個を記録に足した (patterns.json は 857 件。ast の数と記録の static の数を照合する) |
| P3 窓 (pos / endpos) つきの当て方は関数の記録 1,380 件にしか無い | 直す: 本家のパターンに窓つきで当てた記録を足す (大きさの目安の中で)。→ 直した: パターンごとに最初に当たった 8 単位のまわりの窓で search / match を呼び、`window_digest` に残した (89,500 回。golden の合計は約 4.9 MB) |
| P3 落ちないテスト: 結合クラスの安定ソート・ハングルの境界・打ち切りの escape | 直す: 落ちるテストを足す。→ 直した: 同じ結合クラスの 13 字以上の列とハングルの境界 (U+11A7) を記録に足し、変異 (`SortFunc`・`>=`) で赤を確かめた。打ち切りの escape は P2-5 の記録 |
| P3 落ちないテスト: `MaxDepth` | 記録して閉じる: 上限は環境のスタックの量で変わる (README に書いた)。境界の近くは揃うと主張しない |
| P3 2 回の生成が同じ `PYTHONHASHSEED` で、hash の順に依存する出力を見つけられない | 直す: 2 回目の seed を変える。→ 直した: 2 回目は seed を変える。本家は語の一覧から選択肢を set で作るので、パターンの文字列と並びは seed で変わる (当たりの記録は同じ)。パターンの記録だけ文字列と並びを除いて比べる |
