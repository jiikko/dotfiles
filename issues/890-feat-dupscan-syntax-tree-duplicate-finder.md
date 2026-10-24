# 890 (feat): 構文木で重複しているメソッドを repo からあぶり出す道具 dupscan (Go のコア + 言語ごとの差分)

起票日: 2026-10-24

## 概要

repo で実行すると、**テストコード / 製品コードのそれぞれで重複しているメソッド (関数・初期化子・クロージャ) を組にして列挙**し、
どう共通化するかを決める材料 (組のメンバーの場所・どこまで同じか・どこが違うか) を出す道具。
目的は **あぶり出し**で、共通化の修正そのものは人 (か Claude) が出力を見て行う。検査 (lint) として新しい写しを止めるのは
この issue の範囲外で、使う側の repo が決める (obaket は issue 1298)。

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
- **単位の抽出**: adapter が「比べる単位」(本体と戻り値の型) を返す。引数ラベルは本体に出ないので比べない
- **正規化**: 段を分け、段ごとに組を出す
  - L1: 識別子 (ローカル変数・引数・型名) を記号にする。リテラルは残す
  - L2: リテラルも種類ごとの記号にする
  - L3: 値を作る式 (識別子の参照・メンバー参照・呼び出し式) を 1 種類の記号に畳む (プロパティから読む版と直書きの版を同じ形にする)
- **組にする**: 正規化した木のハッシュが同じものを 1 組 (Type-2)。部分木のハッシュの集合の重なりが閾値を超えるものを
  「ほぼ同じ」(Type-3 の一部) として別に出す
- **小さすぎるものを除く**: 下限をノード数・文の数・構文の種類の数で置く。既定値は実測で決める
- **出力**: 組ごとに、メンバー (ファイル:行・関数名・test / prod・所属するターゲットやモジュール)・ノード数・段・類似度、
  **メンバー間のリテラルの違いの表** (L2 以上はリテラルを潰すので「値が同じか」が消える。その判断を表で人に残す)。
  人が読む形と JSON の両方
- **読めない構文**: adapter が ERROR を返した単位は比べる対象から外し、件数と場所を報告に出す (黙って 0 組にしない)

### adapter (言語ごとの差分)

- その言語の構文木 (tree-sitter の文法) から、比べる単位の抽出・ノードの種類の対応 (識別子 / リテラル / 呼び出し / メンバー参照 / 文) を返す
- test / prod の既定の分類規則 (Swift なら `Tests/` 配下・`*Tests.swift`、Go なら `_test.go` など)
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

→ Go + tree-sitter で作る。文法は後者に固定し、版は go.mod の pseudo-version で pin する。

## 受け入れ条件

- [ ] `bin/dupscan <repo>` で、test / prod を分けた組の一覧が出る (人が読む形と `--json`)
- [ ] obaket の PR 24 の前の木から 6 つの `location` の定義を切り出した固定の入力を Swift adapter のテストに置き、段ごとの期待値を書く
      (L1 / L2 で直書きの 4 つが 1 組、L3 で 6 つが 1 組。どの段で「6 つ以外の何が同じ組に入るか」も期待値にする)
- [ ] 必ず 1 組出る入力 (canary) と、読めない構文を含む入力 (除外の件数が報告に出る) をテストに置く
- [ ] 変異で効いていることを確かめる: 6 つのうち 1 つに文を 1 つ足すと Type-2 から外れて Type-3 に残る / 1 つ消すと組が 5 になる /
      正規化の段を 1 つ外すと期待どおりに組が割れる (`mutate-verify` で当てる)
- [ ] コアのユニットテスト (正規化・ハッシュ・組分け・類似度・出力) が adapter に依存せずに書けている
- [ ] obaket の全体に当てたときの組の数・誤検知の割合・ノード数の分布・所要を実測して、この issue に書く (下限と閾値の既定の根拠)
- [ ] CI: `src_dupscan.yml` (`_go-project.yml` を呼ぶ。cgo 有効の macOS runner) で lint とテストが回る。`src/README.md` の一覧に 1 行足す

## やらないこと

- 共通化の修正を自動で行うこと (出力は材料まで)
- 書き方が違って振る舞いが同じもの (Type-4) の検出 (構文木では取れない。LLM などが要る)
- obaket の lint-gates への組み込み (obaket issue 1298)

## 決めること

- 道具の名前 (`dupscan` は仮)
- 「どう共通化するか」の材料をどこまで出すか: メンバーの共通の上位ディレクトリ・同じターゲットかどうかまでか、置き場所の候補まで出すか
- 設定の置き場 (対象・除外・test / prod の規則・閾値。repo の root の設定ファイルか CLI の引数か)
- Swift の次に足す言語 (dotfiles の Go か、他の repo の TypeScript か)

## 関連

- obaket issue 1298 (obaket に入れる側。PR 24 の実例と、既存の `check-test-helper-copies` / 登録簿との関係)
- `src/README.md` (src/ の一覧) / `bin/lib/go_autobuild.zsh` (bin のラッパーが初回にビルドする仕組み) / `.github/workflows/_go-project.yml`

## 進捗

- 2026-10-24 起票。tree-sitter-swift の読み取りを obaket で実測した (上の表)
