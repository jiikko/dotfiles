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

- [x] `make -C src/ja-writing lint test`・root の `make test-lint`・`make test-dupscan-gate` が通り、CI の `src/ja-writing` が緑 (2026-11-13、e5a812461 の run 43296379731。ログに `870 パターン (うち当たったもの 795) を 33644 単位に当てて Python と一致` の行がある)
- [x] `internal/pyre` が、パターンの記録の全パターン・全入力の行で Python の `re` と同じ一致を返す
- [x] `\w` `\s` `\d` `\b` と `re.I` の 1 字の照合が、全コードポイントで Python と同じになる (生成した表で試す)
- [x] `internal/pyunicode` の一般カテゴリと NFKC / NFC が、全コードポイントで Python 3.15.0 (Unicode 17.0.0) と同じになる
- [x] `internal/pyjson` が本家の使う全部の組み合わせで `json.dumps` と同じ字を出し、`json.loads` の受け方・断り方も揃う
- [x] `make golden` が環境を固定して本家を動かし、2 回作って同じ記録になる。取ってきたファイルの sha256 が合わなければ止まる
- [x] 揃わないことを認める範囲の表が `src/ja-writing/README.md` にある
- [x] 実装をわざと壊して落ちることを確かめた記録がある (`mutate-verify`)。最低限は次の 3 つ
  - `\s` の直しを外す
  - 空の一致の規則を外す
  - 入力の集まりを空にする
- [x] 新しく作った検査の仕組み (oracle・記録・突き合わせ) を、敵対的レビュー (opus) に通した

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
| P3 落ちないテスト: 結合クラスの安定ソート・ハングルの境界・打ち切りの escape | 直す: 落ちるテストを足す。→ 直した: 同じ結合クラスの 13 字以上の列とハングルの境界 (U+11A7) を記録に足し、壊し方 (`SortFunc`・`>=`) で赤を確かめた。打ち切りの escape は P2-5 の記録 |
| P3 落ちないテスト: `MaxDepth` | 記録して閉じる: 上限は環境のスタックの量で変わる (README に書いた)。境界の近くは揃うと主張しない |
| P3 2 回の生成が同じ `PYTHONHASHSEED` で、hash の順に依存する出力を見つけられない | 直す: 2 回目の seed を変える。→ 直した: 2 回目は seed を変える。本家は語の一覧から選択肢を set で作るので、パターンの文字列と並びは seed で変わる (当たりの記録は同じ)。パターンの記録だけ文字列と並びを除いて比べる |

### 敵対的レビュー 2 周目 (fable、2026-11-13)

P1 は 0 件、P2 は 3 件、P3 は 6 項目 (1 周目の P2 5 件・P3 の再現は全部直っていた)。P2-A と P3-A は修正の前からある regexp2 の誤り。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P2-A possessive / atomic を、空になれる繰り返しの中に置くと当たりを落とす (`(?:a?+)?a` を "a" に。regexp2 v2.5.2 の誤り) | 直せないので README の「揃わない範囲」に書き、記録に入ったら落ちる検査を足す。本家の検査器 (`check_pattern`) が `?+` `{m,n}+` `(?>` を断ることを本家のコードで確かめた | README に書いた。`TestPatternsMatchPythonRecording` が記録の全パターンを pyre の構文木で数える (`unreliableShapes`。書き方に依らない)。記録に possessive・atomic は 0 個 |
| P3-A 先頭が U+FFFF の字のパターンの search が、開始位置より後ろの一致を落とす (regexp2 の `FindRunesMatch`) | P2-A と同じ | README に書き、同じ検査で U+FFFF を字や集合の端に明示した形を数える。検査器は U+FFFF の字そのものを受ける (`\u` の書き方は断る) ので、`flag-patterns.txt` に U+FFFF を書くとずれうると README に書いた |
| P2-B sandbox の汚れの検査が `TMPDIR` の symlink を素通りする | `filepath.EvalSymlinks` を通してから辿る | 直した。symlink の `TMPDIR` を 3 つの印ごとに断るテスト (`TestNewSandboxRefusesSymlinkedTmpdirUnderMarker`) |
| P3-B sandbox のテストが実装の `upstreamMarkers` を回す | 守る名前をテストに直に書く | 直した |
| P2-C 変数に入れてから渡す literal (`markdown_visibility.py` の `html_until`) を集められず、数の照合は空振りする (印の数は作り方からして必ず同じ) | ast で名前の代入を全部集め、ast と別の数え方 (本家のソースの `re.xxx(` の字面を行ごとに数える) で照合し、読み残しは理由つきの一覧 (`UNREAD_OK` 41 行) にあるものだけを許す。入力の集まりに HTML の塊の文書を足す。README の主張を範囲に合わせる | 直した。literal は 95 → 107 個になり、`-->` `\?>` `\]\]>` `>` と、`</pre\s*>` などの動的なパターンが記録に入った。HTML の塊の開始の判定 (`<(?:script|pre|style|textarea)...`) は 0 件から 12 件に当たる。照合は golden を作るときの Python 側で止める (CI は Python を動かさないので、Go の側は literal の数の下限と、4 つの literal が記録にあることを見る) |
| P3-C 壊しても緑のまま通った新しいコード (M1・M2・M9・M14・M15) | 落ちる入力を記録に足す | `(?i)[\Wk]` ほか IGNORECASE の集合・グループつきの繰り返しの pos > endpos・分岐の共通の先頭の集合を記録に足した (関数の入力 +600)。M15 (`sameNode` の `opAt`) は後述 |
| P3-C 届かないと読まれたコード (`literalPrefixLen` 全体・`searchFilter` の幅 0・`formAdvance` の絞り込み・`formPastEnd` の長さ) | 届かないことを証明してから消す | 消した。証明: 先頭が字の集合 (`opIn`) なら幅は 1 以上、字の接頭辞を数えるのは先頭が字か空の `(?:)` のときだけで、降りた先は字か空になり `first.op != opIn` と同じ空を返す。絞り込みがあるのは空の一致をしないパターンだけなので、空の一致のあとの探索 (`formAdvance`) に要らない。`formPastEnd` は `rs[:pos]` の末尾から探すので一致は必ず空 |
| P3-D lastindex / lastgroup を、本家のパターンについて守る検査が無い | 記録に足す | グループを持つ 106 パターンに `last_digest` (各一致の lastindex と lastgroup) を足し、`TestPatternsMatchPythonRecording` が比べる。全部 Python と一致した |
| P3-E `cmdenv_test.go` の環境変数 `YOMIYASU_CMDENV_TEST` | 直す | `JA_WRITING_CMDENV_TEST` にした |
| P3-F README の古い記述 | 直す | 直した (土台の範囲・`internal/pyre` は作った・パターン数 870・`internal/pyre` の突き合わせ) |
| `PYTHONHASHSEED` でパターンの文字列と並びが変わる (本家が語の一覧の set から選択肢を組む) | 記録の比べ方は変えない。1075 に書く | 1075 の「対応方針」に 1 行足した |

### 敵対的レビュー 3 周目 (codex、2026-11-13)

codex は道具の接続が `timed out negotiating with the code-mode host` で 3 回とも失敗し、コマンドを実行できなかった。差分をプロンプトに入れて読ませたので、指摘はコードを読んだだけの主張で、#1・#7・#8 は main agent が Go と Python で再現した。P1 の指摘 8 件と P3 の 1 件、未確認リスク。

| 指摘 | 扱い | 結果 |
|---|---|---|
| #1 先読みがなくても lastindex / lastgroup が違う (`(?:(?P<early>a*$)\|(?P<late>b))*` を "b" に search。Python は early、Go は late) | 直さない。Python の lastindex は成功した経路の中で最後に実行された閉じの印で、regexp2 は閉じた順を返さない。推定の規則を複雑にしても当たる形が増えるだけ。README の範囲を「先読みの中のグループ」に限らない形 (同じ位置で閉じる 2 つのグループの、実行の順と閉じ括弧の並びの食い違い) に書き直し、本家が使う diff.py の 5 か所は記録の `last_digest` が入力の集まりの上で守ることを書いた | README と `Match.LastIndex` の comment を直した。2 つの例が Python と違うことを再現で確かめた |
| #2 循環する JSON の値で `ValueError` を返せない | 採らない。循環した値を作る経路が無い (本家は渡さず、Go の `Value` は移植したコードが木として組み立てる) | `Dumps` の doc comment に Why not と、作る経路を足すときの検出の条件を書いた |
| #3 長い先頭ゼロの量指定子・条件付きの参照・`\g<番号>` の数字を、Python が桁数の上限 (4300) で断るのに受ける | `pyDecimal` で `int()` と同じ上限を当て、同じ例外の種類と文にする。`invalid group reference` の数は十進に直して出す (先頭の 0 を残さない) | 直した。記録に 4299〜5000 桁の case を足した。`digits-limit-off`・`template-atoi`・`cond-atoi` と名付けた壊し方 (実装をわざと壊して落ちるかを見る) で `TestFunctionsMatchPython` が落ちる |
| #4 oracle の起動で system site の `sitecustomize` が driver より前に走り、誤った記録と生成表を同時に正解にできる | `-S` を足した (`pythonFlags`)。本家の道具と driver が標準ライブラリだけで動くことは、`-S` で全記録を作り直して確かめた。README の `-I` の説明を直した | 直した。`TestPythonFlagsKeepSiteAndCwdOut`。`flags-no-S` の壊し方で落ちる |
| #5 SHA-256 が合う `.py` と別の `.pyc` を実行できる | 検証したバイト列だけを実行ごとの使い捨ての木に写し、そこから import する (`stageTree`)。`-X pycache_prefix` は標準ライブラリの `.pyc` も使えなくなり 675 回の起動が遅くなるので採らない | 直した。`TestStageTreeCopiesOnlyVerifiedFiles` (置き場の `__pycache__` と一覧に無い `.py` を写さない)。実機で、キャッシュに unchecked-hash の偽の `.pyc` を置くと、置き場から import すると偽が動き (対照)、`CallFunctions` では本物のソースが動くことを確かめた (偽の `.pyc` は消した) |
| #6 サロゲートの関数の記録が、入力と期待値を U+FFFD に変えたまま緑になる | 関数の記録の文字列 (入力と結果の全部) を、サロゲートを含むなら `{"ws": [cp...]}` にする (`oracle.WStr`)。Go の比べ方は `encoding/json` で文字列を読み直さず、結果を同じ形に符号化して比べる (`wsEncode`)。JSON の文字列でサロゲートの escape を書いたものは読むときに断る | 直した。サロゲートの case 126 件を記録に足した。`wstr-marshal-plain`・`wstr-accept-lone`・`wsencode-plain` の壊し方で落ちる |
| #7 複数の不正な条件付き参照のエラーが map の順で変わる | 参照の初出の順 (`groupRefs`) で検査する | 直した。12 パターンを記録に足した。`groupref-reverse` の壊し方で落ちる |
| #8 グループ名のエラーの `repr` が Python と違う (改行など) | `pyRepr` を `repr(str)` に合わせた (引用符・短い escape・`\xNN` `\uNNNN` `\UNNNNNNNN`)。印字できるかは一般カテゴリから決める `pyunicode.IsPrintable` で、Python の `isprintable` と全コードポイントで同じことを `unicode.json` の `printable` の範囲 (今回足した) と照らす | 直した。名前 31 種 × 4 経路の case を記録に足した。`repr-raw`・`printable-no-Zs`・`printable-no-space-exception` の壊し方で落ちる |
| #9 「不一致なら何も書かない」を実装もテストも満たしていない | 全部取って検証してから書く 2 段にした | 直した。テストは合った 1 件目も書かれないことを見る。`fetch-write-early` の壊し方で落ちる |
| 未確認 入力のパス (単位の名前) の改行などで digest が別の並びと衝突する | 改行・`\r`・`\|`・`#` を、記録を作る側 (`record_patterns.py`) と読む側 (`LoadCorpus` の `CheckUnitPath`) の両方で断る | 直した。衝突そのものも test にした。`unitpath-off` の壊し方で落ちる |
| 未確認 静的収集が内側の関数・class・内包表記の束縛を外の束縛として拾う | `scope_nodes` で scope ごとの束縛だけを見る (内側の scope は名前と、`nonlocal` / `global` と `:=` だけ)。本家のソースにこの形があるかを、新旧の集め方で 107 パターン・120 呼び出しを比べて確かめた | 直した。本家のソースには無かった (新旧で同じ集合なので `patterns.json` は変わらない)。codex の例と `nonlocal` と class の例は使い捨ての入力で期待どおりに集まることを確かめた (Python を動かす検査は CI に無い) |
| 未確認 `(Pattern, Flags)` の一意性を見ない | `TestPatternGoldenKeysAreUnique` | 足した。`patterns-duplicate-key` の壊し方で落ちる |
| 未確認 `ReadGolden` が記録の後ろの余りを読まない | 2 回目の `Token` が `io.EOF` でなければ断る | 直した。`TestReadGoldenRejectsTrailingData`。`readgolden-trailing-off` の壊し方で落ちる |
| 未確認 `pyre.json` の header を照合しない・`info.py` が prerelease を落とす | 6 つの記録の header が同じ環境であることを見る `TestGoldenFilesShareOneEnvironment`。`info.py` は final 以外に `a` `b` `rc` と番号を付ける (今の 3.15.0 は変わらない) | 足した。`header-env-differs` の壊し方で落ちる。`info.py` の分岐は Python を動かさないと見えないので、final の出力が変わらないことだけ確かめた |
| 未確認 `\B` の検査が件数だけ | 全位置で `\b` の位置と `\B` の位置のどちらか一方にだけあることを見る | 直した。`nonboundary-weak` の壊し方で落ちる |
| 未確認 正規化の「全コードポイント」の digest がサロゲートを除く | README とテストの文言を範囲 (サロゲート 2,048 字を除く 1,112,064 字) に合わせた | 直した |

記録を作り直した差分: `pyre.json` は 315 行の追加だけ (関数の入力のサロゲート・エラーの repr・桁数の上限・参照の順)、`unicode.json` は `props` に `printable` の 1 キーの追加だけ。`patterns.json` `smoke.json` `json.json` `pystr.json` と 2 つの生成表は `-S` と使い捨ての木に変えても同じバイト列だった。作り直しは 2 回、同じバイト列になることも確かめた。

揃わないまま残るもの (本家のパターンに無い): `a{4294967294}` (regexp2 の繰り返しの上限で、Python の MAXREPEAT 未満)。README に書いた。

### 敵対的レビュー 4 周目 (opus、2026-11-13。3 周目の直した差分だけ)

3 周目の 9 項目の直しは、codex の発火条件を Python と Go で当て直して、どれも壊せなかった (#3 は 15,382 件 + 142 件、#8 は全コードポイント、#5 は偽の `.pyc` を置いた実機の実験)。P1・P2 は 0 件、P3 は 3 件。

| 指摘 | 扱い | 結果 |
|---|---|---|
| P3-1 lastindex の「揃わない範囲」に後読みの中のグループが抜けている (`(a)(?:(b)(?<=(a)b))` を "ab" に search すると Python は 3、Go は 2) | README に足す。本家の 5 か所は `last_digest` が守るので、本家との答えの差にはならない | main agent が README を直した |
| P3-2 README の起動の形が `-S` を足す前のまま | 直す | main agent が直した |
| P3-3 使い捨ての木を通す配線 (`RunCLI` / `CallFunctions` / `RecordPatterns`) を守る検査が CI に無い | 記録して閉じる。配線を通すには本家の実ファイルと sha256 の一覧が要り、CI ではネットワークから取れない。配線が関わるのは人が `make golden` で記録を作り直すときだけで、作り直した記録の差分は commit の前に読む。偽の `.pyc` を置く実機の実験で、今は効いていることを確かめた | 閉じた |

3 周目の修正の commit message の「壊し方 19 本」は誤りで、実際は 18 本 (`mutate-verify-list` の一覧の数)。
