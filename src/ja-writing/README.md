# ja-writing (Go)

日本語の文章の読みやすさを点検する本家 [yomiyasu](https://github.com/nanaism/yomiyasu) v3.1.0 (commit `db78b9bc`、MIT) の Go 実装 (skill `ja-writing` の本体。名前は skill に合わせ、本家の写しであることは出典として残す)。
入口は `bin/ja-writing` (1 本の CLI に subcommand `lint` / `diff` / `check` / `hook`)。設計と受け入れ条件は epic 1072 と issue 1073〜1078。

いまあるのは土台 (issue 1073: 骨組み・oracle・Python の標準ライブラリを写す `internal/` の 4 package) だけで、subcommand は `まだ実装していません` と標準エラーに出して rc 2 を返す。
lint は 1075、diff は 1076、check と hook は 1077 で実装する。

## 構成

| 場所 | 中身 |
|---|---|
| `main.go` | subcommand の振り分け (`Main(ctx, env) int`) |
| `cmdenv/` | 各 subcommand が受け取る実行環境 `Env` (引数・標準入出力・cwd・環境変数)。check が lint と diff を子プロセスで動かすか関数で呼ぶかを後で選んでも `Main` の形は変わらない |
| `lint/` `diff/` `check/` `hook/` | 各 subcommand の `Main(ctx, env cmdenv.Env) int` (いまは未実装の stub) |
| `lists/` | 語の一覧 (`references/lists/*.txt` の 8 本) を `go:embed` で持つ。本家と同じバイト列で、`.gitattributes` が改行の変換を止める。SHA-256 は `oracle/upstream.sha256` と照合するテストがある |
| `oracle/` | 本家を正解として動かし、出力を記録して突き合わせる仕組み (下) |
| `testdata/corpus/` | 入力の集まり (下) |
| `testdata/words/` | 語の一覧のフォルダの見本 (`allow.txt` / `flag.txt` / `flag-patterns.txt`)。本家の `--words` の経路を通すために使う |
| `testdata/golden/` | 本家の出力の記録 (`make golden` が作る) |
| `bench/` | ベンチマークの入力を組み立てる `gen.py` と、本家の所要を測る `pybench.sh` (使い方は `bench/README.md`。issue 1082) |
| `LICENSE` `UNICODE-LICENSE.txt` | 本家のもの (MIT と Unicode License V3)。本家の SHA-256 と同じ。`_claude/skills/ja-writing/textlint/LICENSE-yomiyasu` は別の配布物に付くので残している |

## 本家の出力を正解にする仕組み (`oracle/`)

Go のテストは記録と突き合わせるだけで、**CI でも Python を動かさない**。Python が無いときにテストを飛ばして緑にする経路は無い。

1. **取得**: 本家のファイル 15 個 (道具 5・語の一覧 8・LICENSE 2) を固定の commit から `https://raw.githubusercontent.com/nanaism/yomiyasu/<commit>/...` で取る (git clone はしない)。
   SHA-256 の一覧 `oracle/upstream.sha256` と照合する。足りないファイルを全部取って検証してから書く 2 段で、1 つでも合わなければ 1 つも書かずに止まる。置き場は `${XDG_CACHE_HOME:-~/.cache}/yomiyasu-oracle/<commit>/` で、本家のファイルには手を入れない。
   固定する commit を変えるときだけ `make pin-upstream` で一覧を取り直し、差分を読んでから commit する。
2. **環境の固定**: `Runner` は環境変数を空から作り、`PATH` (python3 の dir と `/usr/bin:/bin`)・使い捨ての `HOME`・`TZ=UTC`・`PYTHONHASHSEED=0` だけを渡す
   (継承した `GIT_DIR` などは空から作るので入らない)。cwd と HOME は実行ごとの使い捨ての別の dir。python3 は起動時に 1 度だけ絶対パスで解決する。
   使い捨ての dir は、上の dir に `.git`・`.yomiyasu-words`・`.yomiyasu` があると作らずに止まる (本家は cwd から上へ辿って拾うので、`TMPDIR` が repo の中だと記録が汚れる)。
   辿る前に dir の実パス (`filepath.EvalSymlinks`) にする。本家は `Path.cwd()` (実パス) から辿るので、`TMPDIR` が symlink だと字面の親には無い印を拾うため。
   🚨 起動の形は `python3 -P -s -S -B -X utf8` で、`-I` ではない。`-I` は `PYTHONHASHSEED` を含む `PYTHON*` を全部無視するので、ハッシュがランダムになり、
   set の反復順に依存する出力が実行ごとに変わりうる (手元の 3.15.0 で `hash('a')` が実行ごとに違うことを確かめた)。`-P` は cwd と script の dir を `sys.path` に足さず、
   `-s` は user site を読まない。`-S` は site を import しない (system site の `.pth` と `sitecustomize` は driver より前に走るので、`unicodedata.category` などを差し替えられると、
   誤った記録と、そこから作る表が同時に正解になる)。`-I` が止めたかったものは、env を空から作ることと `-P -s -S` で止めている。本家の道具と driver は標準ライブラリだけで動く (`-S` で全部の記録を作れる)。
   実行する本家のファイルは置き場 (キャッシュ) をそのまま import させず、実行ごとに、`upstream.sha256` と照合したバイト列だけを使い捨ての dir の新しい木に書いてそこから動かす (`stageTree`)。
   `-B` は `.pyc` を書かないだけで、置き場の `__pycache__` にある有効な `.pyc` は読む (SHA-256 は `.py` だけを見る) ので、置き場から import すると、検証したソースと別の bytecode が動きうる。
   新しい木には `.py` と語の一覧しか無く、`.pyc` も一覧に無い `.py` も入らない (`TestStageTreeCopiesOnlyVerifiedFiles`)。`-X pycache_prefix` で `.pyc` を読ませない手もあるが、標準ライブラリの `.pyc` も使えなくなり、675 回の起動が遅くなるので採らない。
3. **記録の 3 つの単位**
   - **パターン** (`py/record_patterns.py`、`patterns.json`): 本家の道具を入力に通す間、`re._compile` を包んで使われた全 (パターン, フラグ) を集める (`re.compile`・`re.search` などが全部通る)。
     集めたパターンを入力の全単位 (ファイルごとの全文 `<path>#doc` と、`"\n"` で割った各行 `<path>#<行番号>`) に `finditer` で当て、パターンごとに
     全単位の一致 (位置は str の添字 = rune 単位、グループの位置つき) を並べた SHA-256 (`digest`) と、当たった単位・一致の数、最初の数単位の中身 (`sample`) を残す。
     本家のソースの `re.xxx(...)` の第 1 引数のうち、定数・定数どうしの `+`・同じ関数か module で文字列の定数を代入した名前 (分岐で別の値を代入するものは全部) を `ast` で読み (実行しない)、
     道具を動かして使われなかったものもパターンに足す (`static`)。変数に入れてから渡すものを含み (`markdown_visibility.py` の `html_until` の `-->` など)、
     分岐の中で代入されても実際には渡されない値 (`html_until = 'blank'`) も足す (記録のパターンが 1 つ増えるだけで害は無い)。
     取りこぼしは ast と別の数え方 (本家のソースの `re.xxx(` の字面の数を行ごとに数える) と比べて止める。ast が第 1 引数を全部は読めない呼び出し (一部が入力や語の一覧で決まる 41 行) は
     `py/record_patterns.py` の `UNREAD_OK` に (ファイル, 行) と理由を持つものだけが残ってよく、一覧に無い読み残しも、読み残しの無い古い行も止まる。
     記録の主張は「読める literal を全部集めた」までで、残る 41 行のパターンは道具を動かして実際に compile されたものだけが入る (入力に当たる HTML の塊・語の一覧を通らなければ入らない)。
     パターンごとに、最初に当たった 8 単位の最初の一致のまわり (`pos` を ±1・`endpos` を ±1) の窓で `search` と `match` を呼んだ結果も SHA-256 にする (`window_digest`)。
     グループを持つパターンは、各一致の `lastindex` (None は -1) と `lastgroup` (None は `-`) を同じ並べ方で縮めた `last_digest` も残す (`Digest.Last`。span の digest には入らない)。
     digest は全部の一致を縮めたもので、`internal/pyre` の `TestPatternsMatchPythonRecording` が同じパターンを compile して `oracle.Digest` で同じ並べ方をして比べる。差が出たら `sample` で位置を見る。
   - **関数** (`py/call_function.py`、`Runner.CallFunctions`): 本家の module を import して関数に JSON の引数を渡し、戻り値か例外を JSON で返す。1 つの Python プロセスの中で順に呼ぶ。
   - **CLI** (`Runner.RunCLI`): 本家の `yomiyasu_*.py` を使い捨ての cwd (入力のファイルを置く) で動かし、stdout・stderr・rc を返す。
   関数と CLI の記録は `smoke.json` に最小の見本 (`--version`・使えない値の例外など) を置いてある。1074 以降は `oracle.Call` / `oracle.CLISpec` の一覧を足して `RecordSmoke` と同じ形で記録する。
   - **標準ライブラリの記録** (`internal/pyunicode`・`internal/pyjson`・`internal/pystr` の正解。`unicode.json`・`json.json`・`pystr.json`): 入力は driver が決まった算法 (乱数を使わない) で作る。
     `py/gen_unicode.py` は全コードポイントの一般カテゴリ・`str` の分類・結合クラス・分解・主合成を読み、文字列どうしの正規化の例を取る。
     `py/record_json.py` は `json.dumps` (本家が使う組み合わせ全部)・`json.loads`・`round` を、`py/record_pystr.py` は `str` のメソッドを、決まった入力に通す。
     `unicode.json` からは `oracle/tablegen.go` が `internal/pyunicode/tables_gen.go` (生成した Go の表。手で編集しない) を作り、`TestGeneratedTablesMatchGolden` が古さを見る。
4. **作り直し**: `make golden` (手で打つ。`make test` には含めない)。各記録に本家の commit・Python と `unicodedata` のバージョンを入れ、
   今の環境と違えば止まる (`UPDATE=1` を付けたときだけ上書き)。記録は `PYTHONHASHSEED` を 0 と別の値にして 2 回作り、同じバイト列になったときだけ書く (所要は約 2 分)。
     パターンの記録だけは、文字列と並びを除いた (digest などの) 組が同じならよい。本家は語の一覧から選択肢を set で作るので、選択肢の並びと、どのパターンをいつ compile するかの順が seed で変わる。
     記録の文字列と並びは seed 0 のもので、当たりの記録 (digest・窓の記録) は seed に依らないことを見る。作り直した記録の差分は commit の前に読む。
   - 入力の集まりを足しただけで、環境が同じなら `UPDATE=1` は要らない。

### 揃わないことを認める範囲 (この表に無い差は全部、不一致として落とす)

| 範囲 | 扱い |
|---|---|
| argparse の usage とエラーの文面 | rc だけを揃え、文面は比べない |
| 本家のコマンド名 (`yomiyasu_lint.py` など) を含む案内の文 | `ja-writing lint` などに置き換えてから比べる |
| check の記録の時刻・ホスト・パス・`python` の欄 | 置き換えの印にしてから比べる (欄の扱いの中身は 1077 で決める) |
| `--version` の文字列 | 本家と同じ `3.1.0` を出す (Go の実装であることは別の欄で出すかを 1078 で決める) |

## 入力の集まり (`testdata/corpus/`)

合計 332 ファイル・1.5 MB・33,312 行 (全文の単位を足して 33,644 単位)。`.gitattributes` で改行の変換を止めている (CRLF の入力を含む)。

| 置き場 | 件数 | 出典 |
|---|---|---|
| `upstream/` | 187 | 本家の `tests/corpus/` の 6 組 (`blacklist_ai` `edge_cases` `human` `raw_ai` `requests` `yomiyasu_rewritten`)。`benchmark_results.json` は除く |
| `upstream-tests/` | 114 | 本家の `tests/test_*.py` と `conftest.py` の中の、非 ASCII を含む 6 字以上の文字列定数を `ast` で抜き出し、ファイルごとに 12,000 字までを空行で区切って並べたもの (pytest は動かさず、ファイルから抜いた) |
| `references/` | 9 | 本家の `skills/yomiyasu/references/` の直下の Markdown 9 本 (例文を多く含む) |
| `dotfiles/` | 7 | この repo の `docs/` の 6 本と `issues/706-...md` |
| `edge/` | 15 | 手で作った端の入力: CRLF・NFD・半角カナと全角英数 (NFKC)・`\x1c`〜`\x1f` と `\x0b` `\x0c` `\x85` `U+2028` `U+2029`・絵文字 (ZWJ・国旗・肌色・キーキャップ)・BMP の外の字・Unicode 16/17 で増えた字・各種の空白・空のファイル・改行なしで終わる行・改行だけ・3,000 字の長い行・引用符と記号・HTML の塊 (`<!-- -->` `<? ?>` `<![CDATA[ ]]>` `<!DOCTYPE>` `<pre>` `<script>` `<style>` `<textarea>`。`markdown_visibility.py` の `html_until` の経路を通すため) |

本家のパターンを集める run (675 回、rc は全部 0): lint を全ファイルに素で (`.md` と `.txt`)・文書の種類 13 種 × 読者つきで・語の一覧 (`testdata/words/`) つきで、
diff を本家の `raw_ai` と `yomiyasu_rewritten` / `blacklist_ai` の組に (素・`--stance` と語の一覧つき・`--endings`)、check の `run --no-record`、hook の `--print-config` と `--version`。

## 記録の件数 (2026-11-13、Python 3.15.0・unicodedata 17.0.0・本家 db78b9bc)

- パターン (本家が使った (パターン, フラグ) の組): **870** (道具を動かして集めたものと、本家のソースの literal 107 個。literal のうち道具が使わなかったものも含む)。
  ソースの literal は 120 呼び出し・重複を除いて 107 個。窓つきの呼び出しは 90,490 回。グループを持ち `last_digest` を持つパターンは 106
- 当たった (パターン, 単位) の組: 1,728,306。一致の総数: 22,143,048 (digest に畳んでいて、記録には残らない)
- 記録の大きさ: `patterns.json` 約 749 KB・`pyre.json` 約 1.8 MB・`smoke.json` 約 3 KB・`unicode.json` 約 886 KB・`json.json` 約 1.1 MB・`pystr.json` 約 657 KB。**合計 約 5.2 MB** (目安 5 MB 前後)。
  生成した表 `internal/pyunicode/tables_gen.go` は約 392 KB
- 実行の件数は `go test ./oracle/` の `TestPatternGolden*` が見る。入力の集まりが 0 件・記録が無い・入力のファイル (SHA-256・行数) が記録と合わない・
  どのパターンも当たっていない・道具の run が 1 つも rc=0 で終わっていない、のどれでも落ちる

## 標準ライブラリを写す package (`internal/`)

| package | 写すもの | 記録 | 件数 |
|---|---|---|---|
| `pyunicode` | `unicodedata` (17.0.0) の一般カテゴリ・結合クラス・NFC / NFD / NFKC / NFKD、`str` の `isspace` `isalpha` `isdecimal` `isdigit` `isnumeric` と `splitlines` の区切り、WTF-8 (単独のサロゲート) の読み書き | `unicode.json` | カテゴリの区間 4,144・NFD 2,081・NFKD 5,914・主合成 961・正規化の文字列 7,383 |
| `pystr` | `str` の `splitlines` `strip` `lstrip` `rstrip` (引数あり・なし)・`isspace` などの分類・`len` | `pystr.json` | 1,622 文字列 |
| `pyjson` | `json.dumps` (`ensure_ascii=True`・`indent`・`sort_keys`・`separators`)・`json.loads(bytes.decode("utf-8"))`・`round(x, n)`・`round(x)`・`repr(float)` | `json.json` | dumps 4,140・loads 1,819・round 7,643・round(x) 503 |
| `pyre` | `re` (str のパターン): `compile` `search` `match` `fullmatch` (`pos` / `endpos`)・`finditer` `findall` `sub` `subn` `split` `escape`・`Match` の `group` `span` `lastindex` `lastgroup` `groupdict`・`Pattern` の `flags` `groups` `groupindex`。regexp2 v2 の上に載せる | `pyre.json` と `patterns.json` | 本家が使った 870 パターン × 33,644 単位・全コードポイントの `\w` `\s` `\d` `\b` と `re.I` の 1 字・関数の入力 3,800 |

- 表は Python の `unicodedata` を読んで作り、Go の `unicode` と `golang.org/x/text/unicode/norm` (Unicode 15.0.0) は使わない。Unicode 16 / 17 で増えた字の分類・合成が違うため。
  正規化は 1 字の結果だけでなく、並べ替えと合成を止める並びを含む 7,383 の文字列で Python と比べる。全コードポイントからサロゲート 2,048 字を除いた 1,112,064 字の 4 形式の結果は SHA-256 で比べる (サロゲートの 1 字の結果は記録に無い。記録の文字列にもサロゲートは入れていない)。
- 単独のサロゲートは WTF-8 (3 バイトの形) で持つ。`json.loads("\"\\ud800\"")` のような入力は Python の str が持てるので、Go の `string` にもその形で入れる。
- `pyjson` が揃わないことがわかっている範囲 (表の外):
  - 入れ子の深さの境界。Python は C のスタックの使用量で `RecursionError` にし、手元の 3.15.0 では深さ 130744 だった。`MaxDepth` はその値で、境界の近くは揃うと主張しない
    (記録は 100,000 の入れ子が読めることと、1,000,000 が `RecursionError` になることを見る)
  - `UnicodeDecodeError` と `RecursionError` のメッセージ (バイトの位置・スタックの使用量が入る)。種類だけを比べる
  - 文字列でない鍵・`int` `float` 以外の数の型・`default=` を持つ `dumps` は持たない (本家は使っていない)
  - 循環した値の `dumps` (Python は `ValueError: Circular reference detected`)。受けない: 循環した値を作る経路が無い (本家は渡さず、Go の `Value` は移植したコードが木として組み立てる)。`Dumps` の doc comment に、作る経路を足すときの検出の条件を書いてある
- `pystr` が持たないもの: `str.lower()` / `upper()` / `casefold()`・`isidentifier()`・`isprintable()` の str としての関数 (字ごとの判定は `pyunicode` の `IsIdentStart` / `IsIdentContinue` / `IsPrintable`。`IsPrintable` は一般カテゴリから決め、全コードポイントで記録と同じことを `TestPropertiesMatchPythonForEveryCodePoint` が見る)。`re.I` の大文字小文字の対応は `internal/pyre` が `pyre.json` から作る表 (`casetables_gen.go`) で持つ。
- `pyre` の作りと揃わない範囲:
  - Python のパターンを自前の構文木 (CPython の `re._parser` の写し) に読み、regexp2 の構文に直す。`\s` `\w` `\d` `\b` と文字の集合は Python の字の集合の範囲の列に開き、`re.I` は `_sre.unicode_tolower` と `re._casefix` の規則で字の集合に開く。名前付きグループは名前の無いグループにして名前を自分で持つ。
    空の一致の規則は、次の探索を `(?:パターン)(?!\G)` で書いて再現する。後読みが固定幅でないパターンは `PatternError`。
  - regexp2 の癖: 先頭が `(?:\A|x)` のような「幅のない選択肢」のあとに「1 字以外」の集合が来ると、regexp2 は後戻りを間違えて当たりを落とす (`(?:^|;)[^a]` が `;ab` に当たらない)。
    補集合の形の集合は 0xFFFF で 2 つのクラスの選択肢に割って渡す (`emitSet`。`TestZeroWidthAlternativeBeforeComplementSet`)。regexp2 を上げるときはこの迂回を外して試す。
  - パターンの記録との突き合わせ (`patterns_test.go`、2,800 万回の照合) は `-race` で時間の上限を超えるので build tag で race から外し、`make test` が race なしで別に回す。
  - 照合に時間の上限は付けない (本家に無い)。入れ子の量指定子による破局的な後戻り (`(a+)+b` など) は本家と同じく戻らない。regexp2 の後戻りの深さの上限は外してある。
  - 本家 (CPython) の癖を写している所: 先頭が文字の集合のパターンの search の開始位置の絞り込みは、全体のフラグの規則で引かれる (`(?a:\W)` は "É" に当たらない。`searchFilter`)。
    空になれる本体の `{m,n}` は m 回のあとに必ず 1 回多く試す (`(a*?){1,2}` の group 1。`{m}` と `{0,n-m}` に割って書く。`unrollRepeat`)。
    pos > endpos の `match` だけが、字を読まない空の一致を返す (`formPastEnd`。末尾の位置の判定・1 字の繰り返し・後読みは満たせない)。
    グループ名の `isidentifier` は `unicode.json` の全コードポイントの表。
    量指定子・条件付きの参照・`\g<番号>` の数字は、`int()` と同じ十進 4300 桁の上限 (先頭の 0 も数える。`sys.get_int_max_str_digits()`) を超えると Python と同じ `ValueError` にする (`pyDecimal`。`strconv` は上限が無く、先頭が 0 の長い数字を受けてしまう)。
    エラーの文に入れる名前は Python の `repr(str)` で出す (引用符の選び方・制御文字・印字できない字の escape。印字できるかは `str.isprintable` と同じ `pyunicode.IsPrintable`)。
    条件付きの参照 `(?(n)...)` の未定義の検査は、本家の dict と同じく参照の初出の順 (番号の順でも map の順でもない)。
    `pyre.json` の関数の記録の文字列 (入力と結果の全部) は、単独のサロゲートを含むなら `{"ws": [cp, ...]}`、含まなければ文字列で持つ (JSON の `"\ud800"` は `encoding/json` が U+FFFD に変え、入力も期待値も別の字のまま比べてしまう。`oracle.WStr`)。
  - 揃わない範囲 (本家のパターン 870 個に無いことは `TestPatternsMatchPythonRecording` が見る): 自分のグループの中から自分を参照する条件付きグループが、繰り返しの 2 周目に入るとき
    (`(?:(x?(?(1)_))[^a])*` を "xyz" に search すると、Python は (0,3)、Go は (0,2))。Python は 2 周目の開き括弧の位置が前の周の閉じ括弧より後ろなら「閉じていない」と読み、regexp2 は前の周の捕獲を読む。
    regexp2 は位置の比較を持たないので写せない。本家のパターンに `(?(` が入ったら落ちる。pos > endpos の `(?m)$` は本家が範囲の外の字を読むので、満たさない (`(?!)`) 扱い。
  - 揃わない範囲 (regexp2 v2.5.2 の誤り。本家のパターンに無いことは `TestPatternsMatchPythonRecording` が pyre の構文木で数えて見る):
    ① possessive (`a?+` `a*+` `{m,n}+`) と atomic (`(?>...)`) を、空になれる繰り返しの中に置いたとき、当たりを落とす (`(?:a?+)?a` と `(?>a?)?a` を "a" に当てると Python は (0,1)、Go は一致なし。
    regexp2 に `(?:(?>a?))?a` を直接渡しても "a" に当たらない。emit.go は possessive を `(?>...)` に直すので踏む)。
    ② 先頭が U+FFFF の字のパターン (`\uffffa` `[\uffff]a` など) の search が、開始位置より後ろの一致を落とす (`\uffffa` を "x\uffffa" に search すると Python は (1,3)、Go は一致なし。先頭からの一致は当たる。regexp2 の `FindRunesMatch` の誤り)。
    利用者が書く `flag-patterns.txt` は本家の検査器 (`markdown_visibility.check_pattern`) を通るので、①は検査器が断る (`?+` `{m,n}+` は「所有の繰り返し」、`(?>` は「原子団」として拒否される。本家のコードと手元の 3.15.0 で確かめた)。
    ②の U+FFFF の字そのもの (`\u` の書き方は検査器が断る) は検査器が受けるので、`flag-patterns.txt` に U+FFFF を書くとずれうる。
  - 揃わない範囲: 繰り返しの回数が regexp2 の上限 (`capture group number out of range`) を超える `a{4294967294}` (Python の上限 MAXREPEAT 未満)。本家のパターンに無い大きさで、`a{4294967295}` 以上は Python と同じ `OverflowError`。
  - 揃わない範囲: `\N{名前}` (字の名前の表を持たない。`PatternError` で断る)・
    `lastindex` / `lastgroup` (下の項)・IGNORECASE の後方参照 (regexp2 の大文字小文字の規則)・
    0xFFFF を超える字の範囲の `re.I` (大文字にする表が無いので、小文字化の逆写像で近似する)・DEBUG フラグは出力しない。
  - `lastindex` / `lastgroup` が揃わない範囲: Python の値は、成功した経路の中で最後に実行された捕獲グループの閉じ。regexp2 は閉じた順を返さないので、Go は
    「一致したグループのうち末尾が最も後ろのもの、同じ末尾なら閉じ括弧が後ろのもの」で推定する。同じ位置で閉じる 2 つのグループの、実行の順と閉じ括弧の並びが食い違う形で外れる
    (`(?:(?P<early>a*$)|(?P<late>b))*` を "b" に search すると、どちらのグループも終端は 1 で、Python は最後に閉じた early (lastindex=1)、Go は閉じ括弧が後ろの late (lastindex=2)。span はどちらも (0, 1))。
    先読みの中のグループが後のグループより遠くまで進む形と、後読みの中のグループ (後ろ向きに実行されるので、終端が手前のグループが後で閉じる。
    `(a)(?:(b)(?<=(a)b))` を "ab" に search すると Python は lastindex=3、Go は 2) も同じ仕組みで外れる。regexp2 が閉じの順を返さない限り、推定の規則を複雑にしても当たる形が増えるだけで、実行の順は復元できないので、規則は直さない。
    本家が `lastindex` / `lastgroup` を使うのは `yomiyasu_diff.py` の 5 か所 (4220・5765・5795・5800・9791 行) で、そのパターンは記録の `last_digest` が入力の集まりの上で守る
    (`TestPatternsMatchPythonRecording`)。入力の集まりに無い入力で外れるかは保証しない。
  - 本家の `flag-patterns.txt` の検査器が受けるパターンの部分集合は、本家のテストの例で確かめる (検査器そのものは 1074 で作る)。
  - `pos > endpos` で `(?m)$` を持つパターンを呼ばないことは、本家の `yomiyasu_diff.py` の `.match(text, pos, endpos)` の 11 か所のパターンがどれも `(?m)` を持たないことをソースで読んだだけで、検査は無い (diff を写す 1076 で、窓つきの呼び出しを写すときに見る)。

## 開発

```sh
make -C src/ja-writing lint test    # CI と同じ (Python は動かさない)
make -C src/ja-writing golden       # 記録を作り直す (Python と ネットワークが要る。UPDATE=1 で環境の違いを許す)
make -C src/ja-writing pin-upstream # 固定する commit を変えるときだけ
```

テストの差し替え: 外部の `python3` を起こす経路は `oracle.Runner` だけで、`subproc.CommandContext` を使う (`exec_boundary_test.go` が見る)。

## 今後 (別の issue)

`internal/pyre` (Python の `re` を regexp2 で写す) は作った。Markdown の構造を読む層 (`markdown_visibility.py` と `flag-patterns.txt` の検査器) は 1074 で作る。
このディレクトリの記録と `oracle.Digest` が、それらの突き合わせの基準になる。
