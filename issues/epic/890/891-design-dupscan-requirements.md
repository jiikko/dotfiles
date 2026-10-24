# 891 (design): dupscan の要件と構想 (epic 890 の正本。作業は同じディレクトリのタスクの子 issue)

起票日: 2026-10-24

## 概要

repo で実行すると、**テストコード / 製品コードのそれぞれで重複しているメソッド (関数・初期化子・クロージャ) を組にして列挙**し、
どう共通化するかを決める材料 (組のメンバーの場所・どこまで同じか・どこが違うか・既にある共通のヘルパーとの一致) を出す道具。
目的は **あぶり出しと知らせること**で、共通化の修正そのものは人 (か Claude) が出力を見て行う。編集の直後に近いものを知らせる hook は範囲内 ([902](902-feat-dupscan-claude-code-hook.md))。
検査 (lint) や hook の block として新しい写しを**止める**のはこの issue の範囲外で、使う側の repo が決める (obaket は obaket issue 1298)。

用途は 2 つあり、どちらも同じコア (走査・parse・単位の抽出) の上に載せる:

1. **重複のチェック**: 既にある写しを組にして列挙する (下の「構成」の大半)
2. **探索のための索引の作成**: これからコードを書く人 (や Claude) が、似たメソッドが既にあるのに探さずに新しく量産するのを防ぐ。
   書く前に「似たものが既にないか」を引ける repo のメソッドの一覧を出す (下の「索引」)。書く前には本体がまだ無いので、引く鍵は名前・署名の型 (戻り値・引数の型)・doc 文になる
   写しは書く前に探さなかったことで生まれるので、1 (できた写しを後から見つける) と 2 (写しを作る前に止める) は対になる

**主な使い方 (ユーザー 2026-10-24)**: Claude Code にコードを変更させている間に、重複したメソッドを作らせない・既存のメソッドを見つけやすくして実装を滑らかにする。
書く前に探す手順 ([903](903-docs-dupscan-search-before-write-procedure.md)。dupscan の索引で名前・署名の型・doc 文を引く) と、Edit / Write の直後に近い既存のメソッドを知らせる hook ([902](902-feat-dupscan-claude-code-hook.md)) の 2 つで効かせる。週次の audit のような後からの洗い出しはその次。

- 名前は仮に `dupscan` (`src/dupscan/` と `bin/dupscan`)
- **Go のコア + 言語ごとの差分 (adapter)** の構成にする。最初の言語は Swift。Go・TypeScript などは adapter を足して広げる
- 名前の違う写し・同じ値を別の書き方で作る写し (下の PR 24 の例) を拾うため、文字列ではなく構文木を正規化して比べる

## 背景

obaket の週次の Claude audit (test-helpers) が出した PR 24 で、同じ `StorageLocation` (`ConnectionID(rawValue: "conn")` /
`.bucket("bucket")`) を組むヘルパーが 6 つ見つかった。3 つは PR が寄せ、残り 3 つはレビューで見つけて寄せた
(1 つは名前が `location(_:)`、2 つは値をプロパティから読む形)。PR 本文は後の 2 つを「別物」として見送っており、
既存の字句の検査 (obaket の `macOS/bin/check-test-helper-copies`) も 6 つのうち 4 つを見ず、2 つは登録簿の事実と違う理由で通していた。
経緯と検査の穴の詳細は obaket issue 1298。

## 既存の道具との関係 (詳細と実測は [901](901-research-dupscan-existing-tools.md))

この要件は当初、既存の道具を調べずに書いた。2026-10-24 の調査で分かったこと (PR 24 の 6 つに当てた実測はまだ無い):

- **名前で引く**手段は LSP (Claude Code の LSP ツール・Serena の MCP) にあるが、言語サーバと索引の待ちが要るので使わない (下の「LSP は使わない」)。名前の検索も dupscan の索引でまかなう。
  jscpd も MCP サーバと、それを使う Claude Code の plugin (DRYwall) があるが、呼んだときにだけ動く
- **本体が同じものを後から見つける**のは CPD・jscpd (トークン方式。Swift に対応) で一部取れる可能性がある。ただし Swift で名前・リテラルの違いを潰すオプションは文書に無く、
  値の書き方の違い (L3) を潰すものは見つからなかった。Go は dupl (値を無視する AST 方式) が dotfiles の 7 module で既に回っている
- **編集の直後に自動で知らせる**組み込みを持つ道具は見つからなかった。主な使い方の中心はここ
- → dupscan が自分で作るのは、Swift の L1〜L3 の正規化・索引・hook (902) を候補とし、901 の実測で範囲を決める。既存の道具で足りる部分は作らない

## 構成 (要件)

### コア (Go。言語に依らない)

- **走査**: 対象の repo の `git ls-files` で列挙する (build の出力・vendor・gitignore 済みを読まない)。symlink は辿らない
  (obaket の `macOS/ObaketTests/Helpers/SharedTestSupport` のように、symlink で別ターゲットに取り込んだ同じファイルを二重に数えない)
- **分類**: 各ファイルを test / prod に分ける (規則は adapter が持つ既定 + 設定で上書き)。報告は test と prod を分けて出し、
  test と prod にまたがる組も別に出す
- **単位**: adapter が「比べる単位」(本体と戻り値の型。引数の名前の集合を添える) を返す。引数ラベルは本体に出ないので比べない
- **ハッシュに入れるもの**: ノードの役割 (下の adapter の対応表)・構造・**匿名トークン** (`+` / `-`、`==` / `!=`、`try` / `try?`、暗黙のメンバーの `.` など)・
  呼び出しの引数ラベル (`rawValue:` など。ラベルまで潰すと、ラベルの違う呼び出しがどれも同じになる)
- **正規化**: 段を分け、段ごとに組を出す
  - L1: 名前を構文上の位置で区別し (参照された名前・型の位置の名前など)、**単位の中で初めて出た順に番号を振る** (一律の 1 記号だと `a == a` と `a == b` が同じになる)。リテラルは残す
  - L2: リテラルも種類ごとの記号にする (文字列の中の式の埋め込みは式として残す)
  - L3: **単位の引数を参照しない最大の部分式**を 1 種類の記号に畳む。`ConnectionID(rawValue: "conn")` も `connectionID` (プロパティ) も同じ記号になり、
    引数を使う `ObjectPath(rawValue: path)` は残る (素直に「値を作る式を全部畳む」と、本体が 1 つの呼び出しである関数は全体が 1 記号に潰れて、何でも 1 組になる)。
    🚨 **引数を取らない単位は本体全体が 1 記号に畳まれる**ので、戻り値の型が同じ引数なしの関数がどれも一致しうる。L3 の組には下限か別の扱いが要る (898 の否定の期待値で決める)
- **組にする**:
  - Type-2 (同じ形): 正規化した木のハッシュが同じものを 1 組
  - Type-3 (少し違う): 部分木のハッシュの**多重集合** (大きさで重みを付ける) の重なりで類似度を測り、閾値を超える対を出す。
    類似は推移的でないので、連結成分で束ねず対 (か全員が互いに閾値を超える集まり) で出す。祖先と子孫の対 (関数とその中のクロージャ) は出さない
- **候補の絞り込み**: obaket の規模で関数と初期化子だけで約 2.2 万 (`func ` 20644 行・`init(` 1423 行。2026-10-24 に git grep で数えた)、
  クロージャを足すと数万。総当たりは数億対になるので、大きさ k 以上の部分木ハッシュの転置索引か MinHash の LSH で候補を絞ってから比べる
- **小さすぎるものを除く**: 下限をノード数・文の数・構文の種類の数で置く。既定値は実測で決める。PR 24 の 6 つは文が 1 つの小さな関数なので、
  既定の下限で落ちないこと (受け入れ条件)
- **出力** (人が読む形と JSON。組ごと):
  - メンバー: ファイル:行・関数名・test / prod・所属するターゲット / モジュール (1 ファイルが symlink で複数のターゲットに属しうるので複数の値)・
    可視性 (private / file 直下 / internal)・呼び出し元の数
  - ノード数・段・類似度
  - **メンバー間のリテラルの違いの表**。L3 で畳んだ式が同じ型の stored property の参照なら、その初期値を 1 段だけ解決して表に出す
    (PR 24 の論点は「プロパティの値が直書きと同じか」だった。プロパティ版にはリテラルが無いので、解決しないと判断できない)
  - 既にある共通のヘルパー (設定で指定した置き場所。obaket なら `SharedTestSupport`) と同じ組なら「既にあるものへ寄せる」と区別して出す
- **読めない構文**: adapter が ERROR / MISSING を含むと返した単位は比べる対象から外し、件数と場所を単位ごと・ファイルごとに報告に出す (黙って 0 組にしない)。
  ERROR を出さずに誤った形で読まれる (`switch await` が別の構文として読まれる等) ものは除外できない。adapter のテストで見つけたら文法の側を直す

### 索引 (探索用。似たメソッドの量産を書く前に止める)

- repo の単位 (関数・初期化子・computed property) を 1 行ずつ: 名前と署名・ファイル:行・test / prod・所属ターゲット・可視性・doc comment の 1 行目・呼び出し元の数
- 既定は全部の単位 (テスト関数そのものは除く)。量産されるのはファイルの中の private なヘルパーなので、共通の置き場所だけに絞らない。
  共通に使われる置き場所 (設定で指定。obaket なら `SharedTestSupport`) にあるものを先に並べ、private なものは「寄せる候補」として区別する
- 形式: grep しやすい 1 行 1 件の Markdown と JSON。commit して置く使い方なら、生成し直した結果との差分を検査で落とせる形にする
  (手で書く索引は実物とずれる。dotfiles の `src/PACKAGES.md` と `scripts/go_packages_index.sh --check` と同じ考え方)
- 索引と重複のチェックは同じ単位の一覧から作り、片方だけが別の抽出をしない (同じ単位が一覧にあって重複の組に無い、の食い違いを作らない)

### adapter (言語ごとの差分)

- tree-sitter の node の種類 (named / anonymous・field 名) から、コアが使う役割 (名前の参照・型の位置の名前・リテラルの種類・呼び出し・
  メンバー参照・文・匿名トークンの字面) への対応表と、比べる単位の抽出を返す
- Swift で役割を決めるのに種類だけでは足りないもの (2026-10-24 に grammar / node-types.json で確認):
  - 暗黙のメンバー `.bucket(...)` は prefix_expression で、操作は匿名トークン `.`。`-x` や `!x` と同じ種類なので、トークンの字面で分ける
  - `Type(...)` は call_expression と constructor_expression のどちらにもなりうる。両方を「呼び出し」に入れる
  - `#expect` / `#require` は macro_invocation (呼び出しとは別)。畳むかどうかを決める
  - 文字列は中に interpolated_expression を持つ
  - trailing closure (`f(x) {}` と `f(x, {})`) は木が別になる。key path (key_path_expression) も別扱い
- **網羅テスト**: node-types.json の named な種類が全部、対応表で分類されている (または明示して無視している) こと。文法の版を上げたときに対応の漏れを黙らせない
- test / prod の既定の分類規則 (Swift なら `Tests/` 配下・`*Tests.swift`、Go なら `_test.go` など)
- 所属ターゲットの解決 (Swift なら `Package.swift` と Xcode の `project.pbxproj`)
- テスト関数の見分け方 (Swift の `@Test` / `func test…`、Go の `func TestXxx`)。テスト関数どうしは `let x = make(); #expect(x == y)` の形で
  大量に一致するので、ヘルパーとは閾値を分けるか既定で除く
- adapter を足すときに、コアを変えずに済むこと (コアのテストは adapter に依存しない)

## 実測 (2026-10-24、obaket の Swift で tree-sitter を試した結果)

使い捨ての Go のプログラムで、obaket (commit 2f35a3b27) の Swift 1626 ファイル (`shared/Sources` / `shared/Tests` / `macOS/Obaket` /
`macOS/ObaketTests` / `macOS/ObaketUITests`。symlink と `.build` / `tmp` を除く) を 1 つずつ parse し、構文木を全部辿って
ERROR / MISSING ノードを含むファイルを数えた:

| 文法と binding | 読めない構文があるファイル | 全ファイルの所要 (470 万ノードを辿るまで) |
|---|---|---|
| `github.com/smacker/go-tree-sitter` 同梱の swift (v0.0.0-20240827094217) | 728 / 1626 (45%)。`#expect` などのマクロ式が読めない | 4.1 秒 |
| `github.com/tree-sitter/go-tree-sitter` v0.25.0 + `github.com/alex-pinkus/tree-sitter-swift` の `with-generated-files` (v0.0.0-20261004031734-82bb3a533e08) | **50 / 1626 (3%)** | 5.0 秒 |

- 後者の内訳: shared の本体 12 / 717、macOS のアプリ 3 / 100、shared のテスト 22 / 463、macOS のテスト 13 / 338
- 読めない構文は `switch await …` / `switch try #require(…)` / `for … where await …` / `nonisolated(unsafe)` など。1 ファイルあたり数か所で、
  tree-sitter はその部分だけを ERROR にして残りを読む
- cgo で文法の C のソースを取り込む (`TOKEN_COUNT` の再定義の警告が 1 つ出る。動作には影響しない)
- 文法の版: Go の binding (`bindings/go/binding.go`) は生成済みの `src/parser.c` を `#include` するので、生成物を commit している
  `with-generated-files` branch を使う。測った版はその branch の先端 (82bb3a5, 2026-10-04)。main の先端 (9e3b87f, 2026-10-13) は新しいが
  生成物が無い。文法の go.mod は go-tree-sitter v0.23.1 を require し、こちらは v0.25.0 を require する (Go の最小版選択で v0.25.0 になる)

→ Go + tree-sitter で作る。文法は `with-generated-files` branch の commit を go.mod の pseudo-version で pin する。上げるときは上の網羅テストを通す。

## 受け入れ条件

この issue (要件の正本) の受け入れ条件は「主な用途に要る子 issue (901・903・902 とその依存) が done になり、決めることが全部決まっていること」。
後回しの 895 と構想の 899・900 は、着手を決めたときに別に数える。
実装ごとの受け入れ条件はタスクの子 issue に置いた (epic 890 の親 issue の表)。

## やらないこと

- 共通化の修正を自動で行うこと (出力は材料まで)
- 書き方が違って振る舞いが同じもの (Type-4) の検出 (構文木では取れない。LLM などが要る)
- obaket の lint-gates への組み込み (obaket issue 1298)

## 決めること

- 道具の名前 (`dupscan` は仮)
- **自分で作る範囲** (901 の実測で決める): コアの正規化を自作するか、CPD・jscpd・ast-grep などを hook から呼ぶ形で足りるか
- hook (902) の置き場所 (dotfiles の `_claude/hooks/` か使う側の repo か) と、1 回の所要の上限
- 索引の置き方: commit して `--check` で守る (897) か、worktree ごとのキャッシュ (902) か、両方か
- 「どう共通化するか」の材料をどこまで出すか: 上の出力に加えて置き場所の候補まで出すか
- 設定の置き場 (対象・除外・test / prod の規則・閾値・既にある共通のヘルパーの置き場所。repo の root の設定ファイルか CLI の引数か)。obaket issue 1298 の運用はこの決定を待つ
- 他のマシン (CI を含む) への配り方: dotfiles の `bin/` が PATH にある前提でよいか、ビルド済みの実行ファイルを配るか
- Swift の次に足す言語の順 (下の「構想: 対応する言語」)

## 構想: 対応する言語 (候補。未確定)

Swift の次は **Go と zsh** を候補にする (ユーザー 2026-10-24)。どちらも dotfiles の中に量が多い。足す前に、Swift と同じく
「対象の repo の全ファイルを読ませて、読めない構文 (ERROR / MISSING) が出るファイルを数える」を測ってから決める。

| 言語 | 構文木の取り方の案 | 懸念 (未確認) |
|---|---|---|
| Go | tree-sitter-go (tree-sitter 公式の文法) で他の言語と同じ adapter にする | 標準の `go/ast` でも正確に取れるが、コアが tree-sitter の木を前提にするなら adapter の形が揃わない。どちらにするかは adapter の境界を決めてから。重複のチェックは dupl (golangci-lint) が dotfiles の Go の 7 module (doctor・glogx・mac-lan-manager・pro-con・ratelimit・treefiler・tuikit) で有効。閾値はどれも 120 トークンで、tuikit 以外はテストを除外 (`src/*/.golangci.local.yml`) ので、Go を足す価値は索引と hook の側にある |
| zsh | tree-sitter-bash を使う案 (zsh 専用の成熟した文法は見つけていない) | zsh だけの構文 (パラメータ展開のフラグ `${(f)...}`・glob の修飾子 `*(N)`・`[[ ]]` の拡張など) を bash の文法は読めないはずで、ERROR が多いと使えない。比べる単位は関数 (`foo() { }` / `function foo { }`) |

- 言語ごとに test / prod の分類の既定 (Go は `_test.go`、zsh は dotfiles の `tests/` 配下など) とテスト関数の見分け方 (Go の `func TestXxx`) を adapter に持たせる
- 言語を足すときは、コアを変えずに adapter と、その言語の node-types.json の網羅テストだけを足す (上の「adapter」の要件)

## LSP は使わない (ユーザー決定 2026-10-24)

dupscan だけで依存も使い方もシンプルになるなら、LSP は使わない (ユーザー 2026-10-24。先に書いた「dupscan と LSP を組み合わせる」構想をこの決定で置き換えた)。
dupscan は自前の構文木 (tree-sitter) だけで、書く前の検索・書いた後の照合・重複の報告をまかなう。

- **書く前の検索**: dupscan の索引 ([897](897-feat-dupscan-index-subcommand.md)) で、名前・署名の型 (戻り値・引数の型)・doc 文を引く。名前の検索も索引でまかなう
  (索引は 1 行 1 件のテキストなので、grep でも引ける)。書く前には本体が無いので、本体の形で比べるのは書いた後 ([902](902-feat-dupscan-claude-code-hook.md))
- **構文木だけでは正確に決まらない値**は、近似と分かる形で出す:
  - 呼び出し元の数: 名前と引数ラベルが一致する呼び出しの数 (別の型の同名のメソッドも数えうる)
  - プロパティの初期値: 同じ型の stored property の初期値を構文木で 1 段だけ解決する ([896](896-feat-dupscan-report-output.md))
  - 所属ターゲット: adapter が `Package.swift` と `project.pbxproj` を読む ([894](894-feat-dupscan-swift-adapter.md))
- **LSP を使わない理由**: 言語サーバの起動と、最初の索引の待ち (obaket の SwiftPM の package で数分・1.6 GB。0 件を返す間を「無い」と読む罠もある) が、
  worktree ごと・言語ごとに要る。gopls の名前検索はあいまい一致で 100 件で打ち切る ([901](901-research-dupscan-existing-tools.md) の実行)。
  dupscan の索引だけで済めば、依存は dupscan の実行ファイル 1 つ、使い方は 1 つになる
- **再開の条件**: 構文木の近似 (呼び出し元の数など) が誤りで判断を誤る実例が出たら、LSP か言語の解析の道具 (Swift の index store・Go の `go/packages`) を
  使う案を [899](pending/899-research-dupscan-lsp-integration.md) で再開する

## 関連

- obaket issue 1298 (obaket に当てる側。PR 24 の実例と、既存の `check-test-helper-copies` / 登録簿との関係)
- `src/README.md` (src/ の一覧と新規プロジェクトのガイドライン) / `bin/lib/go_autobuild.zsh` (bin のラッパーが初回にビルドする仕組み) / `.github/workflows/_go-project.yml`

## 進捗

- 2026-10-24 起票 (issue 890 として)。tree-sitter-swift の読み取りを obaket で実測した (上の表)
- 2026-10-24 LSP と組み合わせる構想を書いた (ユーザー指示: dupscan と LSP を組み合わせてクリーンなコードを目指す)。同日、「LSP は使わない」に置き換えた (下)
- 2026-10-24 Swift の次に対応する言語の候補 (Go・zsh) を構想として書いた (ユーザー指示)
- 2026-10-24 用途を「重複のチェック」と「探索のための索引の作成 (似たメソッドが既にあるのに探さずに量産するのを、書く前に止める)」の 2 つと明記し、索引の要件と受け入れ条件を足した (ユーザー指示)
- 2026-10-24 反証レビュー (規約・事実 / 設計の 2 観点。読み取りのみのサブエージェント) の指摘を取り込んだ:
  - 規約: `src/` の新しいプロジェクトの手順 (Makefile・go.sum・paths・PACKAGES.md・README・ZSH_SYNTAX_FILES・.gitignore)、cgo の lint の所要、文法の版の pin の理由、配り方
  - 設計: L3 は「引数を参照しない最大の部分式」だけを畳む (全部畳むと本体ごと 1 記号に潰れる) / L1 は位置で区別し出現順に番号 / 匿名トークンと引数ラベルをハッシュに入れる /
    Type-3 は多重集合・対で出す・祖先と子孫を除く / 候補の絞り込み (転置索引か LSH) / 既定の閾値で 6 つが出ること・全体に当てた肯定と否定の期待値・
    本物の parser を通す canary・否定の変異 / プロパティの初期値の解決・所属ターゲット・可視性・呼び出し元の数・既存のヘルパーとの一致 / Swift の対応表の落とし穴と網羅テスト
- 2026-10-24 epic 890 にし、この本文を 891 (要件の正本) へ移した。受け入れ条件はタスクの子 issue (892〜900) に振り分けた (ユーザー指示)
- 2026-10-24 主な使い方 (Claude Code の編集の最中の重複の防止と発見性) を概要に書き、「既存の道具との関係」節と「決めること」の 2 項を足した。
  既存の道具の調査は 901、編集の最中に効かせる hook は 902 (ユーザー指示: 既存の道具の考慮不足の指摘と、用途の説明)
- 2026-10-24 反証レビュー (事実の観点) で、dupl の module 数 (3 → 7) と、jscpd の MCP・DRYwall の記載漏れを直した (901)
- 2026-10-24 反証レビュー (設計の観点) を取り込んだ: 書く前は本体の形で引けない (索引は署名の型・doc 文で引く) / 止めると知らせるの書き分け / L3 の引数なしの単位 / 受け入れ条件の範囲 / 索引の置き方を「決めること」へ。書く前の手順は 903 に分けた
- 2026-10-24 「構想: LSP と組み合わせる」節を「LSP は使わない」節に置き換えた (ユーザー決定: dupscan だけで依存も使い方もシンプルになるなら LSP は使わなくてよい)。名前の検索も索引でまかない、構文木で決まらない値は近似と分かる形で出す。899 は pending へ
