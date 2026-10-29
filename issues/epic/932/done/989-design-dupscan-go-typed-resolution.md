# 989 (design): Go の型の解決を go/packages で入れ、def・refs・callers を型で確定する

起票日: 2026-10-30

## 概要

986 の構造の変更。今の構文の近似では、型が要る検索 (Go の定義・参照、受け手の型で絞った呼び出し元) で grep に戻るしかなく、これは方式の壁で越えられない
(`callers New` の 55% が `errors.New` / `rand.New` の雑音。965 F4)。Go について、明示実行の `go/packages` で型を解決する経路を足す。

**ユーザー決定 (2026-10-30)**: 899 の結論「通常探索は構文近似を維持し、LSP・常駐 server・自動 build を導入しない」を、Go について変える。
「今の方式に超えられない壁があるなら、構造改革を視野に入れたい」。899 自身が将来の候補に挙げた「明示実行の go/packages」の範囲で入れる。LSP・常駐 server は引き続き入れない。
型の経路は明示したときだけ走らせ、通常の探索は構文の近似のまま (899 の「自動 build を導入しない」との関係: go/packages は依存の export・cache を作りうる (899) ので、明示した実行に限る)。

## 899 で分かっていること (前提)

- `go/packages` の `LoadSyntax | NeedModule` で、receiver・alias・build tag (`-tags`)・定数を区別できた (899 の「実コードでの裏どり」)
- 型の検査が通らないパッケージは `IllTyped=true` と診断が取れる。899 の方針: 部分的な情報を正常な 0 件として返さない
- 構文の全ファイルと、build 条件で選んだ型つきのファイルは母集合が違う。両方の件数を同じ意味で足さない
- 899 の probe で使った `golang.org/x/tools` v0.52.0 は go1.26.0 を要求する。dupscan の go.mod の最低は 1.25.0 なので、そのままは足せない
- 所要は未測定 (899)

## 設計で決めること (着手時に反証レビューへ通す)

1. **入口**: 既存の `def` / `refs` / `callers` に型の経路を足すか (`-typed` の明示)、別のサブコマンドにするか。既定を型の経路にするかは、所要と壊れた木の扱いを測ってから。
   Go の `def` は 944 (保留。構文で本体つきの定義を返す) と同じ入口を指すので、944 を先にやるか、この issue の型の経路で兼ねるかも決める
2. **壊れた木**: 書いている途中でコンパイルが通らないとき何を返すか。案: そのパッケージは「型で確定できない」と明示して構文の近似の結果を添える (正常な 0 件にしない)
3. **依存と toolchain**: 899 の probe の x/tools v0.52.0 は go1.26.0 を要求し、dupscan の go.mod の最低 (`go` 行) は 1.25.0。CI が使う Go は go.mod の `go` 行ではなく
   `.github/go-version` (今は 1.26.x。`go-version-file` で読む) なので、CI の版の問題ではなく go.mod の最低版の問題。go.mod の `go` 行を 1.26 に上げるか、1.25 で動く x/tools の版を選ぶか
4. **所要**: dotfiles の `src` の module ごとに、型の読み込みにかかる時間とメモリ。hook (書く前の注入) から呼べる速さか
5. **母集合の違いの見せ方**: 構文で見つけた件数と型で確定した件数を、出力でどう分けるか
6. **Swift**: この issue は Go だけ。Swift の index store は 899 の結論のまま (新鮮な store の読み取りだけ) で、別に扱う

## 決めたこと (2026-10-30)

- **go の版 (ユーザー決定 2026-10-30、設計レビューの後に確認し直して維持)**: dupscan 本体の go.mod の `go` 行を 1.26.0 に上げ、`golang.org/x/tools` を足す。CI は既に `.github/go-version` の 1.26.x。
  失うもの (確認のときに伝えた): `bin/dupscan` は pull の後の最初の起動で自動ビルドし、Go 1.25 のマシンでは toolchain (約 90MB) の取得も走る (`bin/lib/go_autobuild.zsh`)。
  その間、書く前の候補の注入 (hook、予算 6 秒) は候補を出せず、失敗が記録されると最長 600 秒 (`GO_AUTOBUILD_FAILED_TTL`) その状態が続く。
  別の module に分ける案 (本体の go.mod を変えない) は採らなかった

## 設計 (反証レビュー (opus) を受けて直した版)

### 最初の範囲と動機

- 最初は Go の `callers -typed` だけ。`def` / `refs` は 944 (Go の def) と入口が重なるので、そちらと合わせて後で決める
- 型が要るのは「変数を受け手にしたメソッド呼び出しを、受け手の型で絞る」こと。`errors.New` / `rand.New` のような `pkg.F` の形は、import の表と照らせば構文だけでも分けられる (933 の `-selector` に近い)。
  型の経路の効果は、受け手の型が違う同名のメソッド (`a.Save()` と `b.Save()`) を分けられるかで確かめる

### 問い合わせと出力

- 問い合わせは修飾した鍵で書く: `<import path>.<名前>` か `<import path>.<型>.<メソッド>` (例: `dupscan/internal/repo.Scope.Whole`)。修飾の無い名前 (`Save`) も受け、その名前の全部の鍵に分けて出す (除くのではなく分ける)
- 出力は鍵ごとに 3 つの区分を出す:
  1. **型で確定した呼び出し**: 呼び出しの式の関数の部分が、その鍵のオブジェクトを指すもの
  2. **呼び出し先を確定できない呼び出し**: interface のメソッドを通した呼び出し (鍵の型がその interface を満たすときの `Saver.Save` 等)、関数値・method value として渡した先での呼び出し。
     899 の方針どおり「unknown」として件数と位置を出し、確定に数えない
  3. **型で確定できないパッケージ**: 読み込みの誤り・`IllTyped`・型の検査の誤り (cgo の失敗を含む)。そのパッケージのファイルは構文の近似の結果を「確定できない」と明示して添える。**正常な 0 件として返さない**
- さらに「この構成で読んでいない Go のファイル」の数を出す: 起点の中の `.go` ファイルのうち、読み込んだパッケージの `CompiledGoFiles` と `IgnoredFiles` のどちらにも無いもの (build tag で丸ごと外れる dir など。`src/treefiler/gorules` は全体が `//go:build ruleguard`)

### 型の読み込み

- module の見つけ方: 起点の各 dir について、上へ辿った最も近い go.mod の module と、起点の下の go.mod の module。`third_party` と `testdata` の下の go.mod は除く (replace 用の stub など)
- module ごとに `go/packages` を 1 回 (`Dir` = module の root、パターン `./...`、`Tests: true`、`NeedName|NeedFiles|NeedCompiledGoFiles|NeedSyntax|NeedTypes|NeedTypesInfo|NeedModule`)。
  test のための変種 (`p [p.test]`・`p_test`) で同じファイルが 2 回読まれるので、呼び出しの位置 (ファイル・行・列) で重複を除く
- module をまたいだ比較は、型のオブジェクトではなく文字列の鍵で行う (module ごとに別々に型を検査するため)。鍵のそろえ方:
  ポインタの受け手 `*A` は `A` に、ジェネリクスは `Origin()` でインスタンス化の前に、埋め込みで昇格したメソッドは選択の受け手ではなく宣言の受け手 (`Obj()` の `Recv`) で作る
- 呼び出しの式の関数の部分は、`ParenExpr`・`IndexExpr`・`IndexListExpr` (`pkg.F[int](x)`) をほどいてから識別子を取る
- build の構成は既定 (今の GOOS / GOARCH・tag 無し) だけ。自動の build はしないが、`NeedTypes` は依存の export data のために `go list -export` で依存を compile する (cgo を含む)。
  明示の `-typed` の実行のときだけ走り、失敗はパッケージ単位で区分 3 に出す

### 測ったこと (2026-10-30、x/tools v0.52.0・go1.26.0、使い捨ての probe)

- dotfiles の `src` の 25 module を順に読み込むと 13.1 秒・最大 86MB (1 回目 15.2 秒)。module 1 つでは 0.5〜0.7 秒 (glogx は 0.70 秒・58MB)。型の誤りのあるパッケージは 0 個
- 明示して打つ `callers -typed` には使える速さ。書く前の注入の hook (予算 6 秒) から `src` 全体を引くには遅いので、hook からは呼ばない

## 受け入れ条件 (案)

- 899 の fixture の形 (A.Save / B.Save / alias / interface / build tag) で、型の経路が呼び出し元を区別する
- 受け手の型が違う同名のメソッド (`a.Save()` と `b.Save()`) を、鍵ごとに分けて出す
- interface を通した呼び出しを、確定に数えず区分 2 に出す
- コンパイルの通らないパッケージで、正常な 0 件を返さない
- 所要の実測を本文に書く

## 反証レビュー (2026-10-30、sonnet、読み取りのみ。987〜989 をまとめて 1 体)

P1 なし。
- P2 (採用): CI の Go の版は go.mod の `go` 行ではなく `.github/go-version` (1.26.x) → 論点 3 を「go.mod の最低版の問題」に書き直した
- P3 (採用): 899 の「自動 build を導入しない」との関係 → 型の経路は明示した実行に限ると書いた / 944 と入口が同じ → 論点 1 に足した
- 反証できなかった: hook の引用、`index -returns` / `-param-type` の実在、899 の事実 (x/tools の版・IllTyped・所要は未測定)、21 件中 4 件、単位約 8,700、ユーザー決定の書き方

## 設計の反証レビュー (2026-10-30、opus、読み取りのみ)

P1 2 件・P2 6 件・P3 2 件。全部を上の設計に反映した (P2-8 はユーザーに確認し直して、決定を維持した)。
- P1: interface の呼び出し・関数値が区分の無いまま黙って消える → 区分 2 を足した / 「errors.New を除く」は定義できない (名前だけの問い合わせでは本物の呼び出し先) → 鍵ごとに分けて出す形と、受け入れ条件を直した
- P2: テストを読まない (今の callers はテストも出す) → `Tests: true` と位置での重複の除去 / build tag で丸ごと外れる dir は件数にも出ない → 読んでいないファイルの数を出す /
  cgo と依存の compile → 区分 3 に出す / 起点が module の下の dir だと module が 0 個・入れ子の stub の go.mod → 上へ辿る・third_party と testdata を除く /
  鍵のそろえ方 (ポインタ・ジェネリクス・埋め込み・IndexExpr) / go の版を上げると hook の index が一時的に空になる → ユーザーに確認し直した
- P3: 933 で足りる部分の切り分け → 動機を「受け手の型で絞る」に言い直した / go.mod の変更は他の module の tidy に影響しない (dupscan に依存する module は無い)
- 崩せなかった: module ごとに読み、文字列の鍵で比べる方式そのもの (replace の先は module のパスで呼ばれ、`./...` は自分の module の package しか読まない)

## 結果 (2026-10-30)

- 実装: commit「feat(dupscan): callers -typed で Go の呼び出し元を go/packages の型から確定する (issue 989)」。`src/dupscan/internal/typed` と `callers -typed`
- 出力: 型で確定した呼び出し (鍵ごと) / 呼び出し先を確定できない (interface を通した呼び出し・関数値として渡した所。修飾した問い合わせでも、名前が同じ interface のメソッドは消さずにここへ出す) /
  型で確定できないパッケージ (自分の誤り) / 依存の誤りで結果が欠けうるパッケージ (走査して数えたうえで注記) / この構成で読んでいない Go のファイル (構文を読んだ位置から決める)
- 本物: `callers -typed Scope.Whole src/dupscan` は 6 件 (1.0 秒)、`callers -typed New src/dupscan` は `errors.New` 43・`dupscan/internal/exclude.New` 16・`crypto/sha256.New` 4・`math/rand/v2.New` 1 に分かれた (0.6 秒)
- 確かめた方法: テスト 10 本・変異 11 本 (どれも狙ったテストだけが赤)・`make -C src/dupscan test lint`・`make test-lint`・ルートの `make test` が rc=0
- 受け入れ条件: 899 の fixture の形 (受け手の違う同名・alias・interface) は 10 本のテストで確かめた。build tag の構成の切り替え (`-tags`) は入れていない (既定の構成だけ。外れたファイルは「読んでいない」に出す)

### 敵対的レビュー (opus) の 1 周目・2 周目 (実装)

- 1 周目 P1 (採用): 修飾した問い合わせで interface を通した呼び出しが消える / build tag で外れたファイル (IgnoredFiles) を読んだに数える
- 1 周目 P2 (採用): cgo の元の .go を読んでいないと誤って出す / 依存の誤りでパッケージが丸ごと消える (IllTyped は依存の誤りでも真)
- 1 周目 P2 (記録・設計の文言を直す): 確定できないパッケージに構文の近似の結果は添えていない (案内の文で構文の callers を勧める) / 関数型のフィールドを通した呼び出しは、関数を渡した所だけを記録する
- 2 周目 P2 (採用): 存在しない型 (`Typo.Save`) でも当たったように見える / interface の method value が消える / 依存の誤りの注記が「数えていない」の見出しに並ぶ / 「読んだ」を一覧から決めている
- 記録 (未対応・P3): 無名の interface のメソッドの鍵が同じ package の関数と衝突しうる / alias の受け手の鍵 (未確認) / 起点を module の下の dir にしても module 全体を読む (確定・Untyped は起点の外も含む) /
  Untyped の Path の名前空間 (module の相対パスと package の ID が混ざる) / 別の module の依存が壊れると could not import で丸ごと外す (同じ module の中なら注記して走査) /
  変種ごとに別の鍵へ解決する同じ呼び出しの重複の除き方 / パッケージの関数への問い合わせ (`a.Save`) でも interface の `Save` が Unknown に出る (受け手を持つ鍵かを見ていない)
- **テストの穴**: cgo の fixture が無いので、「読んだ」を構文の位置から取る直しは変異で赤を確かめていない

### 残り

- `def` / `refs` の型の経路は 944 (Go の def) と入口が重なるので、944 の再開のときに合わせて決める (944 に 1 行足した)

## 関連

- 986 (設計の地図)・899 (LSP・型の解決の調査と結論。この issue で Go について変えた)・944 (Go の def。本体つきで定義を返す。この issue と組み合わせられる)・965 F4 (callers の雑音)・933 (`callers -selector`)
