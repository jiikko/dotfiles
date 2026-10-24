# 890 (feat): 構文木で重複しているメソッドを repo からあぶり出す道具 dupscan (Go のコア + 言語ごとの差分)

起票日: 2026-10-24

## 概要

repo で実行すると、**テストコード / 製品コードのそれぞれで重複しているメソッド (関数・初期化子・クロージャ) を組にして列挙**し、
どう共通化するかを決める材料 (組のメンバーの場所・どこまで同じか・どこが違うか・既にある共通のヘルパーとの一致) を出す道具。
目的は **あぶり出し**で、共通化の修正そのものは人 (か Claude) が出力を見て行う。検査 (lint) として新しい写しを止めるのは
この issue の範囲外で、使う側の repo が決める (obaket は obaket issue 1298)。

- 名前は仮に `dupscan` (`src/dupscan/` と `bin/dupscan`)
- **Go のコア + 言語ごとの差分 (adapter)** の構成にする。最初の言語は Swift。Go・TypeScript などは adapter を足して広げる
- 名前の違う写し・同じ値を別の書き方で作る写し (下の PR 24 の例) を拾うため、文字列ではなく構文木を正規化して比べる

## 背景

obaket の週次の Claude audit (test-helpers) が出した PR 24 で、同じ `StorageLocation` (`ConnectionID(rawValue: "conn")` /
`.bucket("bucket")`) を組むヘルパーが 6 つ見つかった。3 つは PR が寄せ、残り 3 つはレビューで見つけて寄せた
(1 つは名前が `location(_:)`、2 つは値をプロパティから読む形)。PR 本文は後の 2 つを「別物」として見送っており、
既存の字句の検査 (obaket の `macOS/bin/check-test-helper-copies`) も 6 つのうち 4 つを見ず、2 つは登録簿の事実と違う理由で通していた。
経緯と検査の穴の詳細は obaket issue 1298。

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
    引数を使う `ObjectPath(rawValue: path)` は残る (素直に「値を作る式を全部畳む」と、本体が 1 つの呼び出しである関数は全体が 1 記号に潰れて、何でも 1 組になる)
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

- [ ] `bin/dupscan <repo>` で、test / prod を分けた組の一覧が出る (人が読む形と `--json`)
- [ ] 固定の入力: obaket の PR 24 の前の木 (commit 45cc3b208^) から 6 つの `location` の定義を含むファイルを切り出して Swift adapter のテストに置き
      (出典の commit とパスをテストに書く)、段ごとの期待値を書く (L1 / L2 で直書きの 4 つが 1 組、L3 で 6 つが 1 組)
- [ ] 全体の期待値: PR 24 の前の木の全体に **既定の設定 (既定の下限と閾値)** で当て、6 つが出ること、**同じ組に入ってよいもの / 入ってはいけないもの** を期待値にする。
      反証レビューが文法から推した候補: L2 で同じ形になるもの (`BrowserSelectionStateTests.makeLocation` の `"c"` / `.bucket("b")`、`S3StubTestSupport.leaf` の `"connection"`)、
      L3 で入りうるもの (`.container(Self.identifier)` / `.root` / `context.connection.id` を使う版・`StorageResumableMoveUseCaseTests.location(connectionID:path:)`)。どれを入れるかを決めて書く
- [ ] canary: 本物の parser と adapter を通して必ず 1 組出る入力 (core だけの合成の木では adapter の退行を検出しない)。読めない構文を含む入力 (除外の件数が報告に出る)
- [ ] 変異で効いていることを確かめる。肯定と否定を対で置く: 6 つのうち 1 つに文を 1 つ足すと Type-2 から外れて Type-3 に残る / 1 つ消すと組が 5 になる /
      段を外すと期待どおりに組が割れる / 「入ってはいけないもの」が入るように閾値を緩める・畳みすぎる変異で否定の期待値が赤になる (`mutate-verify` で当てる)
- [ ] コアのユニットテスト (正規化・ハッシュ・組分け・類似度・候補の絞り込み・出力) が adapter に依存せずに書けている。adapter の網羅テスト (上)
- [ ] obaket の全体に当てたときの組の数・誤検知の割合・ノード数の分布・所要を実測して、この issue に書く (下限と閾値の既定の根拠)
- [ ] `src/` の新しいプロジェクトの手順 (`src/README.md`「新規プロジェクトのガイドライン」) を満たす:
  - `src/dupscan/Makefile` の `lint:` (`../../scripts/go_module_graph.sh` を呼ぶ) / `test:`、`go.sum`、`src/dupscan/README.md`、`src/README.md` の一覧に 1 行
  - `.github/workflows/src_dupscan.yml` (`_go-project.yml` を呼ぶ。paths に `src/dupscan/**` と共有の script / 設定、replace で取り込む共有 module の dir)
  - `PACKAGES.md` の再生成と全 package の doc comment (`scripts/go_packages_index.sh --check`)
  - `bin/dupscan` を zsh で書くなら root の `Makefile` の `ZSH_SYNTAX_FILES` に登録。go_autobuild の成果物を `src/dupscan/.gitignore` に
  - `make test-lint` (`test-go-project-lanes` ほか) が通る
- [ ] cgo で巨大な `parser.c` をコンパイルするので、CI の lint (`_go-project.yml`。timeout 25 分) と test のコールドの所要を実測して書く

## やらないこと

- 共通化の修正を自動で行うこと (出力は材料まで)
- 書き方が違って振る舞いが同じもの (Type-4) の検出 (構文木では取れない。LLM などが要る)
- obaket の lint-gates への組み込み (obaket issue 1298)

## 決めること

- 道具の名前 (`dupscan` は仮)
- 「どう共通化するか」の材料をどこまで出すか: 上の出力に加えて置き場所の候補まで出すか
- 設定の置き場 (対象・除外・test / prod の規則・閾値・既にある共通のヘルパーの置き場所。repo の root の設定ファイルか CLI の引数か)。obaket issue 1298 の運用はこの決定を待つ
- 他のマシン (CI を含む) への配り方: dotfiles の `bin/` が PATH にある前提でよいか、ビルド済みの実行ファイルを配るか
- Swift の次に足す言語 (dotfiles の Go か、他の repo の TypeScript か)

## 関連

- obaket issue 1298 (obaket に当てる側。PR 24 の実例と、既存の `check-test-helper-copies` / 登録簿との関係)
- `src/README.md` (src/ の一覧と新規プロジェクトのガイドライン) / `bin/lib/go_autobuild.zsh` (bin のラッパーが初回にビルドする仕組み) / `.github/workflows/_go-project.yml`

## 進捗

- 2026-10-24 起票。tree-sitter-swift の読み取りを obaket で実測した (上の表)
- 2026-10-24 反証レビュー (規約・事実 / 設計の 2 観点。読み取りのみのサブエージェント) の指摘を取り込んだ:
  - 規約: `src/` の新しいプロジェクトの手順 (Makefile・go.sum・paths・PACKAGES.md・README・ZSH_SYNTAX_FILES・.gitignore)、cgo の lint の所要、文法の版の pin の理由、配り方
  - 設計: L3 は「引数を参照しない最大の部分式」だけを畳む (全部畳むと本体ごと 1 記号に潰れる) / L1 は位置で区別し出現順に番号 / 匿名トークンと引数ラベルをハッシュに入れる /
    Type-3 は多重集合・対で出す・祖先と子孫を除く / 候補の絞り込み (転置索引か LSH) / 既定の閾値で 6 つが出ること・全体に当てた肯定と否定の期待値・
    本物の parser を通す canary・否定の変異 / プロパティの初期値の解決・所属ターゲット・可視性・呼び出し元の数・既存のヘルパーとの一致 / Swift の対応表の落とし穴と網羅テスト
