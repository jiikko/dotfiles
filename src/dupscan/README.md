# dupscan

repo の中で、本体が同じ形のメソッド (関数・初期化子・クロージャ) を構文木で見つけて組にする CLI (`bin/dupscan`)。
Claude Code にコードを書かせている間に、似たメソッドが量産されるのを見つける。最初の言語は Swift。

設計と狙いは [`docs/dupscan-overview.md`](../../docs/dupscan-overview.md)、同じと判定する方法と誤検知は
[`docs/dupscan-detection.md`](../../docs/dupscan-detection.md)、要件の正本は epic 890 (issue 891)。

## 使い方

```sh
bin/dupscan                      # cwd の repo 全体。重複が無ければ 1 行だけ
bin/dupscan macOS Shared         # 起点を repo の中のディレクトリにする (大文字小文字は git が正規化する)
bin/dupscan -format json .       # 全部の組を JSON で
bin/dupscan -format xcode .      # Xcode / SwiftLint と同じ `ファイル:行:列: warning:` の形式
bin/dupscan -limit 0 -v .        # text で全部の組と、読めない構文の単位の一覧
```

| flag | 既定 | 意味 |
|---|---|---|
| `-format` | `text` | `text` / `json` / `xcode` |
| `-levels` | `L1,L2,L3` | 組を出す段 |
| `-min-nodes` | 10 | これより節の少ない関数・初期化子は比べない |
| `-min-closure-nodes` | 20 | これより節の少ないクロージャは比べない |
| `-limit` | 20 | text で出す組の上限 (0 は全部)。Claude が何度も回すので短くしている |
| `-v` | false | 読めない構文の単位を 1 つずつ出す |

下限の既定値は仮 (issue 898 で obaket と DualNoteApp に当てて決める)。

## 守っていること

- **調べる repo に何も書かない**: git は `subproc.GitCommand` (`--no-optional-locks`) で打ち、`.git/index` も書き直さない。継承した `GIT_DIR` などは外す
- **0 件は判定不能**: 起点に読むファイルが 1 つも無ければ rc=2 で止まる (起点の取り違えで黙って「重複なし」にしない)
- **読めない構文**: tree-sitter が ERROR / MISSING を出した単位は比べず、件数を出す
- 列挙は `git ls-files --cached --others --exclude-standard`。symlink・消えたファイル・submodule の中は読まない
- テスト関数そのもの (Swift Testing の `@Test` / `@Testing.Test`、XCTest の `func test…()`) とその中のクロージャは比べない
- 単位: 関数・初期化子・クロージャ・computed property・subscript・deinit・`didSet` / `willSet`

## 分かっている限界 (誤検知・見逃しの形)

- 局所の名前の有効範囲を見ない (宣言がどこかにあれば、単位の中の同じ名前を全部局所として扱う)。誤検知の向き
- test のファイルの判定はパスの慣習 (`Tests` / `…Tests` / `…Tests.swift`) と `XCTestCase` の継承。`App/ABTests/` のような製品のディレクトリも test と読む (repo ごとの上書きは設定ファイル。issue 906)
- クロージャの鍵は、囲む単位の中での出現の順番。前にクロージャを足すと後ろの鍵がずれる (除外の設定で指すときの注意。issue 906)
- 読めない構文があるファイルでは、読めない部分に飲み込まれた関数は単位として出てこない。件数は 1 行の出力にも出る

## 作り

| package | 役割 |
|---|---|
| `internal/tree` | 言語に依らない単位と本体の木 (役割: 名前の参照・局所の宣言・メンバー・ラベル・型・リテラル・トークン) |
| `internal/norm` | 段 (L1 / L2 / L3) ごとの正規化とハッシュ |
| `internal/group` | Type-2 の組分け (細かい段と同じメンバーの組は粗い段で重ねない・囲む単位が同じ組のクロージャは出さない) |
| `internal/repo` | git でのファイルの列挙と起点の正規化 |
| `internal/swift` | Swift の adapter (tree-sitter の node の種類の分類表・単位の取り出し・test の判定) |
| `internal/report` | text / JSON / Xcode の出力 |

- 文法は `github.com/alex-pinkus/tree-sitter-swift` の `with-generated-files` branch を pseudo-version で固定する。上げたら `TestEveryKindIsClassified` (文法の全部の名前つきの種類が分類表にあるか) を通す
- `third_party/tree-sitter-swift-teststub` は、文法の module のテストが誤った import path で参照する package の空の代わり (go.mod の replace のコメント)
- cgo で文法の C のソース (`parser.c`) をコンパイルする。`TOKEN_COUNT` の再定義の警告が 1 つ出るが動作には影響しない
