# ja-writing (Go)

日本語の文章の読みやすさを点検する本家 [yomiyasu](https://github.com/nanaism/yomiyasu) v3.1.0 (commit `db78b9bc`、MIT) の Go 実装 (skill `ja-writing` の本体。名前は skill に合わせ、本家の写しであることは出典として残す)。
入口は `bin/ja-writing` (1 本の CLI に subcommand `lint` / `diff` / `check` / `hook`)。設計と受け入れ条件は epic 1072 と issue 1073〜1078。

いまあるのは、土台 (issue 1073: 骨組み・oracle・Python の標準ライブラリを写す `internal/` の 4 package) と、lint・diff・check が共有する `markdown_visibility.py` の Go 実装 (issue 1074: `visibility/`) で、
lint の土台と骨組み (issue 1075 の T1〜T4: 絵文字・文末の飾り・文字の下ごしらえ・後処理と出力と CLI・群の呼び出し口。`lint/`) も入っている。
`lint` は動く (引数・終了コード・text と `--json` の出力・文書の指標・40 規則の検出は本家と同じ。40 規則は群 G1〜G20 が全部入っていて、CLI の記録 8,072 件を出力の全体と rc まで全件比べて通る)。`diff` は引数・終了コード・`--endings` の出力が本家と同じ (issue 1076 の群 A・B・C・E。CLI の記録 7,948 件のうち、本家が検査の本体まで進まない 2,382 件を全件比べて通る)。差分と検出の本体はまだで、`--endings` でない検査まで進む呼び出しは `まだ実装していません` と標準エラーに出して rc 2 を返す。check と hook も同じ stub で、1077 で実装する。

## 構成

| 場所 | 中身 |
|---|---|
| `main.go` | subcommand の振り分け (`Main(ctx, env) int`) |
| `cmdenv/` | 各 subcommand が受け取る実行環境 `Env` (引数・標準入出力・cwd・環境変数)。check が lint と diff を子プロセスで動かすか関数で呼ぶかを後で選んでも `Main` の形は変わらない |
| `lint/` `diff/` `check/` `hook/` | 各 subcommand の `Main(ctx, env cmdenv.Env) int` (check と hook はいまは未実装の stub)。`lint/` は本家の `yomiyasu_lint.py` の土台 (T1〜T3) と骨組み (T4)。下の「lint の土台」「lint の骨組みと群の足し方」。`diff/` は本家の `yomiyasu_diff.py` の Go 実装 (群 A・B・C・E まで。下の「diff」) |
| `internal/pycli/` | lint と diff が共有する CLI の部品: argparse の写し (`Spec.Parse`。位置引数とオプションを混ぜてよい形も同じ答え)・使い方の誤りの文・`--fail-on` の値の正規化・出力先が閉じたときの終了コード・入力 (ファイルか標準入力) の読み。本家の lint・diff・check は同じ `_ArgumentParser` と `_read_input` を写しで持つ (ast が同じ) |
| `lint/pausecomma/` | lint の群 G13 (規則 `pause_comma`: 短い主語・主題・目的語のすぐあとの読点)。本家の `_pause_comma_hits`・`_pause_comma_object_hits`・`_pause_comma_line_hits` の写しで、記録は `testdata/golden/pause_comma.json` (issue 1084)。骨組みは `Fallback` と `LineHits` を呼び、G16 は `TENotVerb`、G17 は `TEAttrObject` を使う |
| `visibility/` | 本家の `markdown_visibility.py` (Markdown の読み方・語の一覧・入力の扱い) の Go 実装。本家の名前との対応表は package doc (`visibility/doc.go`)。下の「markdown_visibility の Go 実装」 |
| `lists/` | 語の一覧 (`references/lists/*.txt` の 8 本) を `go:embed` で持つ。本家と同じバイト列で、`.gitattributes` が改行の変換を止める。SHA-256 は `oracle/upstream.sha256` と照合するテストがある |
| `oracle/replay/` | 関数の記録 (digest) と Go の結果を突き合わせるテストの道具 (`ResultDigest`)。oracle 本体に置くと、oracle を import する `internal/pyjson` などの試験と循環する |
| `oracle/` | 本家を正解として動かし、出力を記録して突き合わせる仕組み (下)。`oracle/cmd/golden` が記録を作り、`oracle/cmd/worstcase` が試す数の上限の最悪の組の所要を測る |
| `testdata/corpus/` | 入力の集まり (下) |
| `testdata/diff/` | diff の手で作った入力 (直す前 `old.md` と後 `new.md` の組 117 組と、`--material` に渡す資料)。検出の種類ごとに「当たる組」と「当たらない組」を作った。`testdata/corpus/` に足すと corpus を入力にする全部の記録が作り直しになるので別の置き場にしてある |
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
     1074 で、関数の記録 (`visibility.json`) のために driver に次を足した (どれも指定しなければ前と同じ出力): `get` (関数を呼ばずに定数を返す)・`save` と `{"$ref": 名前}` (戻り値の Python の値を覚え、次の呼び出しへ JSON を経由せず渡す。
     解析の結果を行ごとの問い合わせに渡すため)・`{"$bytes_b64": ...}` と `{"$str": [コードポイント...]}` (bytes と、単独のサロゲートを含む str)・`digest` (結果を正規の JSON にして SHA-256 の先頭 8 字だけを返す。`keep` も付けると結果も返す)・
     `<<ROOT>>` (実行の作業フォルダの印。引数では実パスに、結果では印に戻る)・`op` (作業フォルダへの操作: write・mkdir・symlink・chdir・chmod・remove・setenv・unsetenv・restoreenv・git)。
     Go のテストは同じ操作を `oracle.Fixture` で `t.TempDir()` の実パスにする。
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
- 記録の大きさ: `patterns.json` 約 749 KB・`pyre.json` 約 1.8 MB・`smoke.json` 約 3 KB・`unicode.json` 約 886 KB・`json.json` 約 1.1 MB・`pystr.json` 約 657 KB・`visibility.json` 約 2.5 MB。**合計 約 7.7 MB**。
  生成した表 `internal/pyunicode/tables_gen.go` は約 392 KB
- 実行の件数は `go test ./oracle/` の `TestPatternGolden*` が見る。入力の集まりが 0 件・記録が無い・入力のファイル (SHA-256・行数) が記録と合わない・
  どのパターンも当たっていない・道具の run が 1 つも rc=0 で終わっていない、のどれでも落ちる

## markdown_visibility の Go 実装 (`visibility/`)

lint・diff・check が import する公開面の全部 (`analyze_markdown`・`line_visible_text`・`range_overlaps_protected`・`inline_protected_spans`・`unicode_whitespace`・`decode_source`・`DOC_TYPES`・`READER_LEVELS`・
`normalize_doc_type`・`normalize_reader`・`context_label`・`WordsError`・`resolve_words`・`current_directory`・`words_context`・`words_label`・`input_path_problem`・`absolute_path`) と、その下の
`reference_definition_end`・`find_words_folder`・`load_words`・`check_pattern` (flag-patterns.txt の検査器) を Go の関数にした。本家の名前との対応表は `visibility/doc.go`。
位置は Python の str の添字と同じ rune の添字 (byte の添字と混ぜない)。正規表現は本家と同じパターン・同じフラグで `internal/pyre` を通す。例外は `*visibility.Error` (`Type` が Python の例外の型名、`Message` が `str(exc)`)。

### 本家との突き合わせ (`visibility/oracle_test.go`・`testdata/golden/visibility.json`)

`oracle.VisibilityCases` が公開面の全関数への呼び出し (903 case・240,196 呼び出し) を作り、`make golden` が本家に通して、呼び出しごとの結果 (か例外) の digest を記録する。
digest は結果を正規の JSON (`sort_keys`・`ensure_ascii`・`separators=(",", ":")`) にした SHA-256 の先頭 8 字。dict と list は形ごと、例外は型名と文 (`str(exc)`) ごと比べている。Go のテストは同じ呼び出しを Go の実装に通し、`internal/pyjson` で同じ正規の JSON を作って比べる。
結果そのものを残した呼び出し (小さな入力) は、食い違ったときに記録の中身が見える。

| 入力 | 中身 |
|---|---|
| 入力の集まり (`testdata/corpus/` の 332 ファイル) | ファイルごとに `analyze_markdown` (frontmatter を読む・読まない)・`decode_source` (bytes と str)、行ごとに `line_visible_text` (全部の種類・種類を絞る)・`range_overlaps_protected` (行の範囲・行をまたぐ範囲)・`inline_protected_spans` |
| 手書きの Markdown (105 件) | 柵・frontmatter・HTML ブロックの種類・setext・表・リスト・引用・参照定義・インラインのコード / リンク / 画像 / 裸の URL・CRLF と CR・`<ſcript>` (本家が `AssertionError` になる形) など。結果そのものを残す |
| 乱数で組んだ入力 (400 文書・4,000 のインライン・3,000 の参照定義) | 解析の状態遷移を行の部品の組み合わせで通す。種は固定 (`xorshift`) |
| `decode_source` の bytes | 全 256 バイト・先頭と継続の組み合わせ・途中で終わる形・サロゲートの形 (誤りの範囲と理由が CPython と同じ) |
| `check_pattern` | 手書き 409 (受ける形と、断る理由ごとの形)・単独のサロゲートを含む 7 (`Required` がサロゲートを保つ) と、26 字の全部の 3 字までの並び (18,278) と 11 字の全部の 4 字の並び (14,641。`re.compile` の誤りも含む) |
| 語の一覧 | 本物の `git init` / `git worktree add` で作る 3 つの形 (通常の checkout・linked worktree・`--separate-git-dir` と `core.worktree` の共有の checkout)・手で書いた git の形 52 種 (config・config.worktree・commondir・.git ファイル。config の行を割る U+0085・U+2028・U+2029・VT・FF・FS・GS・RS を含む)・`--words` の読み方と `~` の展開 (HOME を変えて)・`load_words` の中身の 66 種 (BOM・CRLF・1 万行・行の中の U+0085・U+2028・U+2029 などは行を割らない・UTF-8 の誤り・タブ・正規表現の上限・フォルダ・リンクの輪・読めない権限)・`words_context` / `words_label` |
| パスと cwd | `input_path_problem` (点で始まる名前・作業フォルダの外・symlink・輪・cwd が symlink の先・cwd が消えたとき)・`absolute_path`・`current_directory`・`unicode_whitespace` (U+0000〜U+3100 の全部と端の字)・定数 17 個 |

- fixture は `oracle.Op` の並びで、Python の driver と Go の `oracle.Fixture` が同じ操作をする。git の引数は `subproc.GitArgs` を通し、環境変数は空から作る (`oracle.GitEnv`。利用者の git の設定・認証を持ち込まない)。
  テストの作業フォルダは `t.TempDir()` の実パスで、上に `.git`・`.yomiyasu-words`・`.yomiyasu` があると、探索が上まで届いて結果が置き場所で変わるので、本家の使い捨ての dir と同じく止まる (`TMPDIR` をその外に向ける)。
- 空振りを止める検査 (`TestRecordingCoversEveryPublicFunction`): 公開面の関数ごとに記録の件数が 0 でないこと、例外を投げうる関数は、例外になった呼び出しと成功した呼び出しの両方があること、記録に公開面の一覧 (`oracle.VisFunctions`) にない名前がないこと。
- 保存できなかった `save` (例外で終わった呼び出し) の `Ref` は `KeyError` になる。case の名前を付けた名前にしてあるので、別の case の同じ名前の値を読むことはない (`scopeRefs`)。

記録の関数の件数 (呼び出し数 / 例外になった数): `analyze_markdown` 5,689 / 4・`line_visible_text` 75,719 / 20・`range_overlaps_protected` 67,853 / 2・`inline_protected_spans` 38,166 / 0・`reference_definition_end` 3,241 / 6・
`unicode_whitespace` 12,574 / 0・`decode_source` 2,413 / 1,299・`normalize_doc_type` 30 / 8・`normalize_reader` 10 / 4・`context_label` 25 / 16・`find_words_folder` 268 / 79・`load_words` 93 / 42・`resolve_words` 16 / 4・
`words_context` 6 / 0・`words_label` 6 / 0・`check_pattern` 33,335 / 27,764・`input_path_problem` 92 / 0・`absolute_path` 40 / 1・`current_directory` 5 / 0・定数 17 個 (各 1)。
(`line_visible_text` と `range_overlaps_protected` の例外には、解析が例外で終わった後の `Ref` の `KeyError` が混ざる。)

### 揃わない範囲

- Windows の分岐 (ドライブ・`USERPROFILE`) は写さない (macOS だけが対象)。
- `HOME` が未設定のときの `~` の展開 (本家は `pwd.getpwuid(os.getuid()).pw_dir`、Go は `os/user.LookupId`) は記録に入れない: 結果に実行した人のホームのパスが入り、機械ごとに記録が変わる。
  手元 (macOS・uid 501) では、`HOME` を外して `CGO_ENABLED=1` と `0` のどちらでも、両方が同じホームを返すことを確かめた。環境を作れる範囲 (HOME を変える・空にする) は記録の `option` の case にある。
- `words_context` と `words_label` に `None` を渡す形は写さない: 本家の注釈は `dict` で (`Optional` ではない)、本家の呼び出し元 (check・lint・diff の 7 か所) は `words is not None` の後にだけ呼ぶ。Python は `TypeError` で落ちるだけで、契約ではない。
- 関数の引数の型は、本家の注釈どおりの型だけを受ける (`normalize_doc_type` の引数は str か None だけで、`str(value)` で文字列になる int などは受けない。`decode_source` は str か bytes で、bytearray は別に持たない)。
- `OSError.strerror` の文は macOS の `strerror(3)` の表で持つ (EPERM・EIO・EACCES・EISDIR・ELOOP・ENAMETOOLONG など 20 種。記録で確かめたのは EISDIR・ELOOP・EACCES・ENAMETOOLONG。ENOENT と ENOTDIR は文を出さず、ファイルが無いものとして扱う)。表に無い errno は `Unknown error: N`。
- `str.lower()` は、ASCII の語と比べる用途に限って写す (`pyLowerASCII`: 小文字化で ASCII になる非 ASCII の字は U+212A だけ)。ほかの用途には使えない。
- `check_pattern` の「Python が警告を出す書き方」の分岐は、Go にない: 走査が `[ ]` の中の入れ子と `--` `&&` `~~` `||` を先に断るので、走査を通った書き方で 3.15.0 の `re.compile` が警告を出す形は見つかっていない
  (26 字の 3 字までの並びと手書きの警告の形を本家に通して確かめた)。
- `*Analysis` の `LineVisibleText` と `RangeOverlapsProtected` は、結果の覚え (本家の dict の `_line_visible_cache` と `_kind_mask_cache`) を書くので、複数の goroutine から同時に呼ばない。
- `getcwd` は `syscall.Getwd` に、字面を `os.Stat` した結果と `.` の `os.Stat` が同じ実体か (`os.SameFile`) の検査を足している (macOS の `syscall.Getwd` は、消えたフォルダ、または消して作り直したフォルダの元のパスを返し、libc の `getcwd` (Python の `os.getcwd`) は ENOENT で失敗する。記録の `paths` の case の `gone` と `reborn` で確かめた)。
- `ReferenceDefinitionEnd` は、行の中に改行の字がある入力で、本家の `IndexError` を `error` (`*Error`) で返す (本家の `_RowCursor` は最後の行でも `\n` を返し、次の行の添字で落ちる)。`analyze_markdown` が渡す行は改行を持たないのでこの経路には届かない。記録の `reference-definition-end` の case に改行つきの行を足してある。
- `InputPathProblem` の `os.path.realpath` は、本家と同じ規則 (存在しない部分は字面のまま・リンクの輪は手前で止める) を Go で書き直したもので、Python 3.15.0 の `posixpath.realpath` の移し。macOS の NFC / NFD の同じフォルダは `os.SameFile` (dev と ino) で見る。

### 試す数の上限 (`PATTERNS_MAX_COST = 512`) の判断

本家は上限を、Python の `re` で 10 万字の行に最短一致の繰り返しを重ねた組を当てる時間 (手元の macOS で約 0.7 秒) から決めている。値は `visibility.PatternsMaxCost` などに同じ値で写した
(`PATTERN_MAX_COST` = 512・`PATTERNS_MAX_COST` = 512・`PATTERNS_MAX` = 500・`PATTERN_MAX_LENGTH` = 200・`PATTERN_MAX_REPEAT` = 30・`PATTERN_NAME_MAX` = 40・`FLAG_REPLACEMENT_MAX` = 80・`WORDS_MAX_LINES` = 10,000)。
上限ちょうど (合計 512 を通し、513 を断る) の境界は記録の `words-load` の case (`patterns-cost-512`・`patterns-cost-513-total`・`patterns-cost-total-over-line2`・`patterns-cost-single-over`) で本家と同じ判定になることを見ている。テストの合否には時間を使わない。

`go run ./oracle/cmd/worstcase -rounds 5 -shape <組>` が、同じ入力 (`a` が 99,999 字と `d` の 1 行) を Python (本家の `check_pattern` と `search`) と Go (`CheckPattern` と `internal/pyre` の `Search`) で、5 回ずつ交互に測る
(奇数回目は Go を先に起こす。毎回新しいプロセス)。起動は、壁時計から driver (Go は子プロセスの `-child`) の中の所要を引いた値で、Python はインタプリタの起動と本家の module の import、Go はバイナリの起動と package の初期化を含む (driver の中の所要は、どちらも import・初期化の後から測る)。
2026-11-13 (他の session が走っていて load average 12〜15 のときの測定)、Apple M3 Max・macOS・Python 3.15.0・go 1.26.0 (秒。最小 / 中央 / 最大):

| 組 (試す数の合計はどれも 512) | Python の起動 | Python の search | Go の起動 | Go の search |
|---|---|---|---|---|
| `single`: `a{0,7}?[ab]{0,7}?[ac]{0,7}?d` (最短一致を 3 つ重ねる。字の集合を変えて畳めなくした) | 0.035 / 0.037 / 0.046 | 0.425 / 0.442 / 0.447 | 0.009 / 0.009 / 0.010 | 0.237 / 0.241 / 0.242 |
| `word`: `\w{0,7}?[a-z]{0,7}?\S{0,7}?d` (`\w` と `\S` は Python の字の集合を範囲の列に開くので集合が大きい) | 0.036 / 0.037 / 0.039 | 0.475 / 0.483 / 0.491 | 0.008 / 0.009 / 0.009 | 0.645 / 0.657 / 0.674 |
| `greedy`: `a{0,7}[ab]{0,7}[ac]{0,7}d` (最長一致) | 0.036 / 0.038 / 0.040 | 0.158 / 0.161 / 0.164 | 0.008 / 0.010 / 0.010 | 0.052 / 0.053 / 0.054 |
| `split`: 試す数 64 のパターン 8 つ (合計 512) | 0.036 / 0.037 / 0.039 | 0.374 / 0.378 / 0.384 | 0.008 / 0.009 / 0.009 | 0.253 / 0.254 / 0.258 |

`check_pattern` (compile) はどちらも 1 ms 未満。本家の module の import は 0.013〜0.014 秒 (Python の起動に含まれる)。
`a{0,7}?a{0,7}?a{0,7}?d` のように同じ字の繰り返しを重ねる形は、regexp2 が 1 つの繰り返しに畳んで 0.005 秒で終わり、Python は 0.31 秒かかった (`-shape merged`)。
**判断: 上限は 512 のまま据え置く。** Go で測った最悪は `word` の 0.66 秒 (Python の同じ組は 0.48 秒で 1.4 倍) で、本家が上限を決めた根拠の約 0.7 秒を超えない。
測ったのは macOS の手元 1 台だけで、CI の macOS runner では測っていない (本家が上限を決めた 1,024 は CI の Linux で 2 秒を超えたという記録があるが、この repo の CI は macOS)。
lint (1075) が `check_pattern` の `Required` (必ず当たる字の並び) で行を絞ってから正規表現を当てるなら、最悪の行はさらに限られる (未確認: 1075 の実装次第)。

## lint の土台 (`lint/`。issue 1075 の T1〜T3)

候補を出さない部品。規則の群 (G1〜G20) と後処理・出力 (T4) がこの上に載る。本家の名前と Go の名前の対応は、各ファイルの冒頭のコメントが正本 (本家のファイルを更新したら、そこの表で見直す関数を引く)。

| 群 | ファイル | 本家 (`yomiyasu_lint.py`) | 公開 |
|---|---|---|---|
| T1 絵文字 | `emoji.go`・`emoji_tables_gen.go` (生成) | `_emoji_matches`・`_emoji_character_class`・`_emoji_flag_pattern`・`EMOJI_PATTERN`・`_EMOJI_START_PATTERN`・表 (Unicode 18.0) | `lint.EmojiMatches(text) []string` (本家の `_emoji_matches`。diff も同じ数え方を使う) |
| T2 文末の飾り | `endmark.go` | `_merged_ranges`・`_bracket_opens_sentence`・`_strip_end_decoration`・`_decoration_breaks`・`_with_undecorated`・`_end_quote_closers`・`_end_closers_for` | なし |
| T3 文字の下ごしらえ | `prose.go` | `_mask_quoted_segments` (と `_sparse`)・`_question_content_end`・`_last_context_sentence`・`_ends_with_question`・`_prose_visible_text`・`_image_only_row`・`_plain_sentence_records`・`_rule9_context`・`_v220_scan_text` | なし |
| 共通 | `pyregexp.go`・`ordered.go` | `_LazyPattern` (初めて使うときに組む正規表現)・`dict.fromkeys` (重なりを除いて初めて出た順を保つ) | なし |

- **照合を正規表現にしない所**: 絵文字は `EMOJI_PATTERN` (8,514 字) を pyre で組まず、区間の表と 5 つの枝 (旗・キーキャップ・U+FE0F つきの基底・テキスト表現の基底・そのほか。本家の選択肢の順) を手で書く。
  文の切り出しの `(?<=[。！？])` と `(?<=[。！？!?])` の split は、記号の直後で切る手書きの走査 (空の一致で切るので、末尾が記号なら最後に空の要素が出る)。
  `$` を使う `_END_BRACKET_AFTER_PREDICATE` と、先読みを持つ `_WH_QUESTION`・`_V220_*`・`_END_BREAK_*` は pyre を通す (使うときに組む `lazyPattern`)。
- 本家は analysis の dict に `_prose_line_cache` などの覚えを書き足す。Go は `visibility.Analysis` に欄を足さず、lint の文脈 `document` に持つ (1 つの文書の 1 回の実行だけで使う)。
- 本家の `core is not sentence` (飾りを除いたか) は、`stripEndDecoration` の 2 つ目の戻りの bool にしてある。除いた結果が元と同じ文字列になることは無い (除くと必ず短くなる)。

### 本家との突き合わせ (`lint/oracle_test.go`・`testdata/golden/lint-base.json`)

`oracle.LintBaseCases` が土台と各群の関数への呼び出し (494,482 回。803 case) を作り、`make golden` (`-only lint-base` で約 15 秒) が本家に通して、呼び出しごとの結果の digest を記録する。形は `visibility.json` と同じ。
`analysis` を取る関数は、`markdown_visibility.analyze_markdown` の結果を `save` し、`Ref` で渡す (結果の記録は要らないので、driver の `quiet` で digest を 8 つの 0 にする)。

| 入力 | 中身 |
|---|---|
| 入力の集まり (332 ファイル。うち本家の `analyze_markdown` が `AssertionError` で落ちる 2 件を除く) | 行ごとに `_emoji_matches`・`_strip_end_decoration` (closers なし・あり)・`_decoration_breaks`・`_with_undecorated`・引用の伏せ 2 本・疑問の判定 3 本・`_prose_visible_text`・`_image_only_row`・`_v220_scan_text`・`_end_closers_for`・閉じかっこごとの `_bracket_opens_sentence`。文書ごとに `_plain_sentence_records`・`_rule9_context`・`_end_quote_closers` |
| 手書きの文書 (visibility の 106 件のうち 104 件と、画像だけの行・引用の閉じ・指示の残りの文脈・飾りの行など 13 件) | 上と同じ呼び出しで、結果そのものを残す |
| 絵文字 | 手書き 104 (旗・キーキャップ・U+FE0E / U+FE0F・肌色・ZWJ・タグ・サロゲート)・全コードポイントを後ろの字 5 通りと並べた走査・旗の全 676 組・表の関数 (`_emoji_character_class`・`_emoji_flag_pattern`・`_merged_ranges`) |
| 文末の飾りと疑問の文 | 手書きの文 (飾りの文 89・引用と疑問の文 97・単独のサロゲート 5)・開きかっこを見る 256 字の境目の前後 55 |
| 定数 21 個 | 絵文字の表 3 本と旗の地域・文末の飾りの区間・かっこと引用符の対・疑問の語尾・表から作る文字クラス |

- 照合: 整数の鍵の dict (`_rule9_context` と `_end_quote_closers`) は、本家の `sort_keys=True` が整数の大小で並べてから文字にするので、Go は鍵を数の順に入れた `pyjson.Object` を並べ替えずに書く。
- 絵文字の表 (`emoji_tables_gen.go`) は `oracle.GenerateLintEmojiTables` が `lint-base.json` の定数から作る (手で編集しない。`oracle` の `TestLintEmojiTablesMatchGolden` が見る)。
  Go が組む正規表現の文字列 (`EMOJI_PATTERN` など) は、パターンの記録 (`patterns.json`) に同じ文字列があることを `TestPatternSourcesAppearInPatternRecording` が見る。
  表と枝で書いた `EmojiMatches` は、`EMOJI_PATTERN` の文字列を pyre で組んだ `findall` とも、入力の集まりの全文で同じ答えを返す (`TestEmojiMatchesAgreesWithPatternFindall`)。
- 空振りを止める検査 (`TestLintBaseRecordingCoversEveryFunction`): 関数と定数ごとに記録の件数が 0 でなく、例外を使わない (analysis を作れない入力は `lintAnalysisFails` で外す)。
- 実装をわざと壊して `bin/mutate-verify-list` で確かめた 22 本 (絵文字 4・文末の飾り 7・文字の下ごしらえ 10・重なりの除去 1)。20 本が red (うち 1 本は最初は緑で、入力を足して red にした)、2 本は等価と判断した (理由は issue 1075 の進捗)。

## lint の骨組みと群の足し方 (`lint/`。issue 1075 の T4)

`ja-writing lint` は動く (`lint.Main`)。引数と終了コードは本家の argparse と `_run` の確かめる順 (`--version` → 条件 → 種類と読者 → words の同時指定 → 語の一覧 → 入力のパス → 入力を読む) をそのまま写し、text と `--json` (format_version 2・鍵の順・`indent=2`・`ensure_ascii`) の出力も本家と同じ。
40 規則は群 G1〜G20 が全部入っている。文書の指標 (`metrics`) と語の一覧の `context` も本家と同じ値を出す。

| ファイル | 中身 |
|---|---|
| `lint.go` | `Main`・`run` (本家の `_run`)・`--strict` / `--fail-on` の判定 (`failConditions`・`hitConditions`・`exitLine`)・入力の読み方 (`readInput`) |
| `args.go`・`help.txt` | argparse の写し (`parseArgs`・`usageError`)。`-h` の文面は本家が出したものをそのまま持つ (`help.txt`) |
| `lint_text.go` | `lintText` (本家の `lint_text` 5512-5988)・`detect` (検出まで)・`eachRow` (行のループ)・`beginRow`・`bodySteps` |
| `post.go`・`result.go`・`rules.go` | 検出の後 (種類で外す・allow・ダッシュの上限・点数・件数)・出力 (`--json` の値・text の `renderReport`)・種類ごとの扱い (`docTypeSkipRules` ほか) |
| `g01_metrics.go` ほか `g02`〜`g20` | 群ごとの検出 (全部入っている)。G1 の指標 (`metricsAndListGate`) と G8 の窓 (`g08Window`) は骨組みも使う。G14 は `g14_calque.go` (型 1〜5・calque の message・手掛かりの行の絞り込み)・`g14_label.go` (metaphor_label)・`g14_lists.go` (一覧と手掛かりの語の正規表現)・`g14_tables.go` (本家が組み終えた表) |

### 群を足す人へ

1. **自分のファイルだけを書く** (`g<番号>_*.go`)。骨組み (`lint_text.go`・`post.go`・`result.go`) は書き換えない。群の口は、本家の `lint_text` と同じ順に骨組みが呼ぶ lintState のメソッド:

   | 群 | 口 | 呼ばれるとき |
   |---|---|---|
   | G1 | `g01Document()` | 行のループの前。指標 (`s.metrics`・`s.listGateChars`) から excess_bold・excess_list |
   | G2 | `g02SentenceEnd()` | 同上 (`s.sentences` が段落の文の記録) |
   | G3 | `g03Bold()` | 同上。issue 1083 の `lint.BoldFindings(s.text, s.an)` を呼ぶ |
   | G4・G5・G6 | `g04Emoji(r)`・`g05Dash(r)` (表と引用でなく、文書にダッシュがあるとき)・`g06Heading(r)` (見出しの行。続けて骨組みが `g08Vocabulary`) | 行ごと |
   | G7〜G13 | `bodySteps` の順 (`g07TrailingColon`・`g08Vocabulary`・`g08Translated`・`g09Filler`・`g09InfoSentences`・`g10InstructionResidue`・`g11ShortSentences`・`g12Negation`・`g13PauseComma`) | 表・見出し・引用でない行ごと。事前の口は `g12Prepare`、事後の口は `g12Document`。`g12RecordBoldLabel(r)` は行の文を切る前 |
   | G14〜G20 | `g14WordFindings`・`g15RenyoFindings`・`g16AiPhraseFindings`・`g17SlideCommaFindings`・`g18ShapeFindings`・`g19DocumentFindings`・`g20ProjectTermFindings` | 行のループの後、本家の `analysis["_findings"] = findings` 以降の順 (`s.findings` はそれまでの候補) |

2. **行をまたぐ状態を持つ群は、自分の `g<番号>State` に持ち、`lintState` に欄を足す** (いまは G11〜G13。本家の行のループの外の変数と、事前の口が作る表)。持つ状態の無い群は欄を作らない。骨組みが持つのは複数の群が読むものだけ: `previousSentence`・`kikuLines`・`metrics`・`rule9`・`sentences`・`findings`。
   候補は `s.add(newFinding(rule, line, severity, message, snippet))` で、本家が足す順に足す (足す順が `--json` の `findings` の順)。内部の欄 (`Words`・`Objects`・`Heads` など) は `finding` の欄で、持つ欄は nil でない値を入れる (空でも持つなら `[]string{}`)。
3. **検出を、本家の検出の生の並び (`testdata/golden/lint-raw.json`) と比べる**。`TestRawFindingsMatchPythonRecording` が、40 規則の全部の候補を検出の順のまま比べる (規則の一覧は `lint/rules_test.go` の `ruleSet`。そこにない規則の候補を Go が出すと落ちる)。
   CLI の記録 (全ファイル × 種類・読者・語の一覧・text と `--json` の出力の全体と rc。8,072 件) は `TestCLIMatchesPythonRecording` が全件比べる (stdout は SHA-256 の先頭 16 字と長さ・stderr は全文・rc)。
   群が別の群の検出を読む組 (G11 が G9 の filler_patterns・G14 が G8 の候補・G17 が G13 の pause_comma の `_heads` と `_objects`) は、読む側のコードが `s.findings` を引く。
4. allow.txt で語を一部だけ外したときに message を作り直す関数 (`calqueMessage` (G14)・`renyoMessage` (G15)・`aiPhraseMessage` (G16)・`projectTermMessage` (G20)) は、その群のファイルにある。
5. 記録に足りない入力の形 (地図 4.3 節) は、`oracle/lintrawgap.go` の `lintRawGapInputs` に足す (検出の生の並びの記録 `lint-raw.json` にだけ入る。`rawgap/` の下に置かれ、全部の文脈で通る)。
   入力の集まり (`testdata/corpus/`) に足すと、lint-base・visibility・lint-cli・lint-raw の記録が全部変わる。足したら `go run ./oracle/cmd/golden -only lint-raw` で作り直し、既存の行が変わっていないことを差分で読む。
   足した入力は `lintRawGapInputs` の注釈を読む (G1〜G20 の境目と、入力の集まりで当たらない語・型の項目を 1 行ずつ。G14 の分は `lintrawgap_g14.go`)。群を入れる人は、自分の群の表に入力の集まりで当たらない項目が残っていないかを、本家の表を 1 項目ずつ外した結果の比べで数えて確かめる (G14 は 1,085 項目を数え、等価な 319 項目の理由は issue 1075)。

### 本家との突き合わせ (T4)

| 記録 | 中身 | 比べるテスト |
|---|---|---|
| `lint-cli.json` (8,072 件) | 入力の集まり 332 ファイル × (`--json`・`--doc-type` 13 種・`--words`・`--no-words`・text・`--strict`・`--fail-on` の組) と、誤りの呼び出し・標準入力・作業フォルダを変えた語の一覧の探し方の 95 件。stdout は SHA-256 の先頭 16 字と長さ、stderr は全文、rc | `TestCLIMatchesPythonRecording` (40 規則が全部入ったので全 8,072 件を比べる。検出の生の並びが空の入力だけを比べる段は終わった) |
| `lint-raw.json` (3,232 件) | 全ファイルと足した入力 72 (`rawgap/`) × 8 つの文脈 (種類なし・essay・slides・promo・script・minutes・article・語の一覧) の、種類・allow・ダッシュの上限で外す前の候補 (内部の欄を含む。「規則:行:digest」の列) と、文書の指標 (`metrics`) の digest | `TestRawFindingsMatchPythonRecording` (指標は全件、候補は入った規則だけ)・`TestRawRecordingRuleIDsMatchRuleSet` (記録に出る規則 ID が 40 の表と同じ)・`TestEveryRuleHasHitAndMissInputsInRecording` (規則ごとに当たる入力と当たらない入力がある)・`TestGapInputsHitInRawRecording` (足した入力が語・型ごとに 1 件当たる) |
| `lint-post.json` (85 件) | 本家の `lint_text` の検出を、作った候補に差し替え (driver が `_project_term_findings` を包む)、種類で外す・allow・ダッシュの上限・点数・件数・context・`--json` の形・`render_report` だけを本家のまま走らせた結果。`--json` は鍵を並べ直さない出力の digest も持つ (鍵の順の違いは正規の JSON の digest では見えない) | `TestPostProcessingMatchesPythonRecording` (検出の群が入る前に、候補のある入力で後処理と出力を確かめる唯一の比較。`allow-partial|` の 8 件は、allow で語の一部だけを外して message を作り直す形で、群が全部入ったので全件比べる) |

- 記録の driver は 3 つ (`oracle/py/record_lint_cli.py`・`record_lint_raw.py`・`record_lint_post.py`)。CLI の driver は 1 つの Python のプロセスの中で `main(argv)` を何度も呼ぶ (stdin・stdout・stderr は 1 回ごとに作り直し、作業フォルダの実パスは `<<ROOT>>` に戻す)。
  `go run ./oracle/cmd/golden -only lint-cli|lint-raw|lint-post` で 1 つずつ作り直せる (それぞれ約 40 秒・10 秒・数秒)。
- 本家のファイルには手を入れない。検出の生の並びは、`yomiyasu_lint._project_term_findings` を driver が包んで取る (`lint_text` は module の global から名前を引く)。
- 揃わない範囲:
  - `-h` の文面は本家が出したもの (端末の幅が 80 桁のとき) を `lint/help.txt` に持つ。`COLUMNS` で幅を変えても Go は変わらない。
  - argparse の写しは、本家の記録にある形 (`--opt=値`・`--`・`-` ・負の数 (Python 3.15 の `-\.?\d` の前方一致。Unicode の数字を含む)・空白を含む引数・`-h` の連結・省略形の拒否・値の欠け・知らないオプション・無視した値の repr の引用) を確かめている。ほかの形 (`-hx` 以外の短い名前の連結など) は未確認。
  - `--fail-on` の値の小文字化は `strings.ToLower` に İ の分解を足したもの。語末の Σ を ς にする規則と、Python の Unicode のバージョンとの差は写さない (使えない値の文に出る字だけに現れる)。
  - 本家の rc 3 のうち、トレースバックの文面と、Windows の閉じたパイプ (`EINVAL`) は写さない。
  - 読む側が閉じたパイプ: 本文の出力は本家と同じ rc 3 と 1 行。`-h`・`--version`・使い方の誤りは、本家では書き込みが Python の終わり際の flush まで遅れるので、
    同じく rc 120 (標準出力なら「Exception ignored while flushing sys.stdout:」の 2 行) にする (Python 3.15.0 で実測)。EPIPE を受けるため、入口 (`main.go`) が SIGPIPE を `signal.Notify` で受ける。
    EPIPE 以外の書き込みの誤り (ENOSPC など) は内部エラーの rc 3。
  - 入力のパスの検査 (`visibility.InputPathProblem`) とファイルを開く処理は、プロセスの cwd を見る。語の一覧の探し方だけが `Env.Cwd` を見る。`Main` を cwd の違う `Env` で呼ぶテストは、プロセスの cwd も移す。
  - allow で語の一部だけを外すときの message の作り直し (`calqueMessage`・`aiPhraseMessage`・`projectTermMessage`・`renyoMessage`) は、本家に作り直させた記録 (`lint-post.json` の `allow-partial|`) を置いてあり、`TestPostProcessingMatchesPythonRecording` が全件比べる。
  - `run` は検査の本体を引数で受ける (`lintFunc`)。rc と「終了コード1」の行の配線は、候補を注入した `TestRunExitCodeAndExitLine` が見る (CLI の記録は全件を比べる)。
  - 結果の text は `result` の構造体から作る (`renderReport`)。check (1077) が子の JSON を読んで `render_report` を呼ぶ形が要るときは、JSON の値から作る入口をそのとき足す。

## diff (`diff/`)

本家の `yomiyasu_diff.py` (11,577 行) の Go 実装 (issue 1076)。群の一覧と順は issue 1076 の「群と順」。いま入っているのは群 A (CLI の骨組み・引数と終了コード・`-h`・`--version`)・B (9 キーの下ごしらえ)・C (文末の種類と立場)・E (太字。lint と共有する `internal/bold`) で、
`--endings` の出力が本家と同じ。差分と検出の本体 (本家の `diff()` と出力) はまだで、`--endings` でない検査まで進む呼び出し (引数と入力の誤りが無いもの) は `まだ実装していません` を返す。
公開の関数は本家の公開名に対応する (`Normalize` `HasList` `Sentences` `Paragraphs` `LogicPoints` `BareEnd` `Register` `EndingKind` `EndingUnits` `StanceFlags` `EndingChanges`)。本家の `count` (どこからも呼ばれない) と `_bold_short` (`internal/bold` が持つ) は写さない。

| 記録 | 中身 | 比べるテスト |
|---|---|---|
| `diff-cli.json` (7,948 件) | 入力の集まりの直す前と後の組 (upstream の `raw_ai` と書き直した文・`blacklist_ai`・`_bl` の付いた書き直し 72 組) と手で作った入力 117 組を、text・`--json`・`--fail-on meaning-risk` (text と `--json`) で。オプションを通す組 (upstream の先頭 6 組と手で作った入力のうち重くないもの) は文書の種類 13 通り・読者・立場 3 通り・語の一覧・資料も。`--endings` は入力の集まりの全ファイルと手で作った入力の全ファイルを立場なしと 3 つで。誤りの呼び出し・`-h`・`--version`・標準入力・作業フォルダを変えた語の一覧の探し方も。stdout は SHA-256 の先頭 16 字と長さ、stderr は全文、rc。`--json` の出力は何が出たか (`kinds`) も持つ | `TestCLIMatchesPythonRecording` (検査の本体が入るまで、本家が本体まで進んだ呼び出しは「比べていない」と数えてログに出す。それ以外は全件比べ、`bodyImplemented` が真になったら比べていない件数が 0 でなければ落ちる)・`TestDiffRecordingHasHitAndMissForEveryKind` (検出の種類 71 種ごとに、出る入力と出ない入力が記録にある) |
| `diff-base.json` (789 case・65,952 呼び出し) | 群 B・C・E の公開の関数 (`normalize` `has_list` `sentences` `paragraphs` `logic_points` `bare_end` `register` `ending_kind` `ending_units` `stance_flags` `ending_changes` と、diff 側の `bold_problems`) を、入力の集まりの全ファイル・手で作った入力・組ごと・手書きの文に通した結果の digest | `TestBaseMatchesPythonRecording` (全呼び出しを比べる)・`TestBaseRecordingCoversEveryFunction` |

- 記録の driver は `oracle/py/record_diff_cli.py` (lint の CLI の driver と同じ作り)。作り直しは `go run ./oracle/cmd/golden -only diff-cli|diff-base` (約 70 秒・約 12 秒)。
- 手で作った入力は `testdata/diff/<名前>/` (`old.md`・`new.md`・資料は `material.md` か `material1.md` から)。長い行・大きい文書の入力 (`full-budget` など) は 1 回の実行が重いので、オプションを通す組から外してある。
揃わない範囲 (lint の「揃わない範囲」と同じ扱いのもの。表の外の差は不一致として落とす):

- `-h` の文面は本家が出したもの (端末の幅が 80 桁のとき) を `diff/help.txt` に持つ。`COLUMNS` で幅を変えても Go は変わらない。
- 本家の rc 3 のうち、トレースバックの文面は写さない (1 行目 `yomiyasu_diff.py: 内部エラー: <型>: <文>` までは揃える)。
- 標準エラーの案内の文のコマンド名は本家の `yomiyasu_diff.py` のまま (1078 で lint とまとめて決める)。
- `--fail-on` の値の小文字化は lint と同じ (`strings.ToLower` に İ の分解を足したもの。語末の Σ と Python の Unicode のバージョンとの差は写さない)。
- 語の一覧は埋め込みを使うので、JSON の `list` はいつも `references/lists/<名前>.txt`、`list_problem` は `null`。本家が隣のファイルを読めないときの内蔵の一覧と、一覧の注意の節は写さない。

## 標準ライブラリを写す package (`internal/`)

| package | 写すもの | 記録 | 件数 |
|---|---|---|---|
| `pyunicode` | `unicodedata` (17.0.0) の一般カテゴリ・結合クラス・NFC / NFD / NFKC / NFKD、`str` の `isspace` `isalpha` `isdecimal` `isdigit` `isnumeric` と `splitlines` の区切り、WTF-8 (単独のサロゲート) の読み書き | `unicode.json` | カテゴリの区間 4,144・NFD 2,081・NFKD 5,914・主合成 961・正規化の文字列 7,383 |
| `pystr` | `str` の `splitlines` `strip` `lstrip` `rstrip` (引数あり・なし)・`isspace` などの分類・`len` | `pystr.json` | 1,622 文字列 |
| `pyjson` | `json.dumps` (`ensure_ascii=True`・`indent`・`sort_keys`・`separators`)・`json.loads(bytes.decode("utf-8"))`・`round(x, n)`・`round(x)`・`repr(float)` | `json.json` | dumps 4,140・loads 1,819・round 7,643・round(x) 503 |
| `pyre` | `re` (str のパターン): `compile` `search` `match` `fullmatch` (`pos` / `endpos`)・`finditer` `findall` `sub` `subn` `split` `escape`・`Match` の `group` `span` `lastindex` `lastgroup` `groupdict`・`Pattern` の `flags` `groups` `groupindex`。regexp2 v2 の上に載せる | `pyre.json` と `patterns.json` | 本家が使った 870 パターン × 33,644 単位・全コードポイントの `\w` `\s` `\d` `\b` と `re.I` の 1 字・関数の入力 3,800 |
| `difflib` | `difflib.SequenceMatcher(None, a, b, autojunk=False)` (`New[T]`。字の列と文字列のリスト) と本家の `_ExactSequenceMatcher` (`NewExact`。difflib と同じ答えを長い文書で速く出す照合) | `difflib.json` | 559 入力 (字の列 478・リスト 76・使い回し 2。うち本家の速い照合だけを通した大きい入力 6)・本家の照合の呼び出し 15 か所 |

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
  - 本家の `flag-patterns.txt` の検査器が受けるパターンの部分集合は、`visibility/` の `CheckPattern` と記録の `check_pattern` の呼び出し (約 3.3 万) で確かめた。
  - `pos > endpos` で `(?m)$` を持つパターンを呼ばないことは、本家の `yomiyasu_diff.py` の `.match(text, pos, endpos)` の 11 か所のパターンがどれも `(?m)` を持たないことをソースで読んだだけで、検査は無い (diff を写す 1076 で、窓つきの呼び出しを写すときに見る)。

- `difflib` の作りと揃わない範囲 (issue 1081):
  - 本家は照合を全部 `(None, a, b, autojunk=False)` で作るので、`isjunk` と `autojunk` の引数は持たない。記録の driver (`py/record_difflib.py`) は本家のソースを `ast` で読み、
    この形でない呼び出し・`difflib.` の `SequenceMatcher` と `Match` の外の属性・Go が持たないメソッド (`get_grouped_opcodes` など) があれば止まる。`FindLongestMatch` は範囲が並びの中の呼び方だけを受ける (外は panic)
  - driver は同じ入力を Python の difflib と本家の `_ExactSequenceMatcher` に通し、2 つが違えば止まる。difflib が長さの 2 乗で遅い大きい入力 (同じ字の 8 万字の繰り返しなど) は本家の照合だけに通す
  - 本家の速い照合の Go の写しは、求め方の選び方 (閾値と順) を本家と同じにし、Go に無い道具の所だけ置き換えた: Python の `hash(str)` → 1 字ずつずらす多項式の hash (法 2^61-1、128 ビットの積で剰余)、
    `str.find` → `[]rune` の KMP、`_common_prefix_len` の倍々の切り出し → 1 字ずつの比較、`_first_of_length` の短い長さの枝 → 長さによらず多項式の hash。hash は表を引く手がかりにだけ使い、引いた位置は必ず字で比べる
  - Go の中の試験 (`exact_test.go`) は閾値を小さくした matcher と hash を弱くした matcher で、短い入力でも全部の求め方 (19 の枝) を通し、`New` と同じ答えを確かめる
  - 所要 (2026-11-13、M3 の Mac、照合の処理だけ。起動は含まない。同じ入力を Python の 2 つと Go の 2 つで交互に 3 周。周ごとの揺れは 2% 以内):

    | 入力 | 字数 (a / b) | Python difflib | 本家の速い照合 (Python) | Go `New` | Go `NewExact` |
    |---|---|---|---|---|---|
    | 同じ段落を 400 回繰り返して 1 つ直す | 22,400 / 22,403 | 1.10 秒 | 0.013 秒 | 0.027 秒 | 0.007 秒 |
    | 入力の集まりの直す前と後を全部つないで 3 回 (全面の書き直し) | 31,455 / 34,827 | 2.44 秒 | 0.106 秒 | 0.063 秒 | 0.018 秒 |
    | 同じ字の 8 万字の繰り返しで 1 字違い | 80,001 / 80,001 | (測らない。長さの 2 乗) | 0.45 秒 | (測らない) | 0.050 秒 |

## 開発

```sh
make -C src/ja-writing lint test    # CI と同じ (Python は動かさない)
make -C src/ja-writing golden       # 記録を作り直す (Python と ネットワークが要る。UPDATE=1 で環境の違いを許す)
                                    #   TMPDIR は、上に .git・.yomiyasu-words・.yomiyasu が無い場所に向ける (repo の中の tmp/ は使えない)
go run ./oracle/cmd/golden -only visibility   # markdown_visibility の関数の記録 (visibility.json) だけを作り直す (約 10 秒)
go run ./oracle/cmd/golden -only lint-cli     # lint の CLI の記録 (lint-cli.json。約 40 秒)。-only lint-raw (検出の生の並び) と -only lint-post (後処理) も同じ形
go run ./oracle/cmd/golden -only lint-base    # lint の土台の関数の記録 (lint-base.json) と絵文字の表 (lint/emoji_tables_gen.go) だけを作り直す (約 15 秒)
go run ./oracle/cmd/worstcase -rounds 5 -shape single   # 試す数の上限の最悪の組の所要を本家と Go で測る (テストの合否には使わない)
make -C src/ja-writing pin-upstream # 固定する commit を変えるときだけ
```

テストの差し替え: 外部の `python3` を起こす経路は `oracle.Runner` だけで、`subproc.CommandContext` を使う (`exec_boundary_test.go` が見る)。

## 今後 (別の issue)

`internal/pyre` (Python の `re` を regexp2 で写す) と、Markdown の構造を読む層 (`visibility/`。`markdown_visibility.py` と `flag-patterns.txt` の検査器) は作った。
lint (1075。土台の T1〜T3 は入った)・diff (1076)・check と hook (1077) がこの 2 つの上に載る。このディレクトリの記録と `oracle.Digest` が、それらの突き合わせの基準になる。
