# 901 (research): 既存の道具の調査 — dupscan を作る前に、使えるもの・足りないものを確かめる

起票日: 2026-10-24

## 概要

[891 (要件の正本)](../891-design-dupscan-requirements.md) は、既存の道具を調べずに書かれていた (ユーザー 2026-10-24「既存ツールに関しては考慮不足」)。
ここで既存の道具を調べ、使えるものは使い、dupscan が自分で作るのは足りない部分だけにする。

比べる軸は、主な用途 (ユーザー 2026-10-24) に合わせる: **Claude Code にコードを変更させている間に、重複したメソッドを作らせない・
既存のメソッドを見つけやすくして実装を滑らかにする**。そのため、後から重複を見つける道具だけでなく、エージェントが引く手段 (LSP・MCP) と、
編集の直後に知らせる組み込み (hook) も対象にする。

## 調べた道具 (2026-10-24)

出典は Web の一次情報 (README・公式文書) と、手元での実行。**obaket はこのマシンに無いので、PR 24 の 6 つに当てた実測はまだ無い** (下の受け入れ条件)。

| 道具 | 方式 | Swift / Go / zsh (bash) | 名前の違う写し | 値の書き方の違い (L3) | Claude との組み込み | 確かめ方 |
|---|---|---|---|---|---|---|
| PMD CPD | トークン列の一致 | ○ / ○ / × | `--ignore-identifiers` と `--ignore-literals` は公式文書上 Java・C++ だけ。Swift と Go は名前・リテラルを潰せない | × | なし (CLI。text・xml・csv・markdown を出す) | 公式文書 |
| jscpd (v5 は Rust) | トークン列 + Rabin-Karp | ○ / ○ / ○ (bash) | `--mode` は strict (全トークン)・mild (改行と空白を除く。既定)・weak (さらにコメントも除く)。どの mode も識別子は潰さない (v4 の README) | × | 出力の形式に `ai` がある。組み込みの MCP サーバ (`jscpd --mcp <path>`) と Agent Skills もある | README (v5.4.1・v4.0.5) |
| dupl (golangci-lint) | AST の型だけの列を比べる (値は無視) | × / ○ / × | ○ (名前と値を無視) | 一部 (値を無視するので直書きの違いは潰れる。プロパティとの違いは木の形が違う) | なし | 文書。**dotfiles の Go の 7 module (doctor・glogx・mac-lan-manager・pro-con・ratelimit・treefiler・tuikit) で有効。閾値はどれも 120 トークンで、tuikit 以外はテストを除外 (`src/*/.golangci.local.yml`)** |
| DRYwall (Claude Code の plugin) | jscpd を呼ぶ | jscpd の対応に従う (例は JS / TS) | jscpd と同じ (潰さない) | × | skill `/drywall:scan`・重複を寄せる agent・MCP tool。**呼んだときに動く**。編集の直後に自動で走る仕組み (PostToolUse) は README に無い | README |
| mizchi/similarity | 関数の AST の木の編集距離 (TSED) | × / 試験的 (similarity-generic) / × | ○ | 類似度として一部 | 出力を AI に渡しやすい形にしており (AI-friendly output)、AI にリファクタの計画を立てさせるプロンプトを同梱。自動で知らせる組み込みは無い | README |
| ast-grep | tree-sitter の構造パターンで検索 | ○ / ○ / ○ (bash) | パターンの変数で吸収 | パターン次第 | MCP や skill で使える。類似度は出さない (「この形を探す」の問い合わせ) | crate の言語一覧 |
| Claude Code の LSP ツール | 言語サーバの `workspaceSymbol` / `findReferences` / `incomingCalls` | sourcekit-lsp / gopls / (bash-language-server は未導入) | × (名前で引く) | × | **Claude Code に組み込み済み** | 手元で実行 (下) |
| Serena (MCP) | LSP を MCP で出す | ○ / ○ / ○ (40 以上の言語) | × (名前で引く。ファイルの概要・参照元) | × | MCP で Claude Code に繋がる | README |
| 埋め込みの意味検索の MCP (semantic-code-mcp など) | tree-sitter で切って埋め込み、意味で引く | Python・Rust・Markdown だけ (Swift・Go・Bash は非対応) | ○ (意味で引く) | ○ (意味で引く) | MCP。埋め込みのモデルと索引の置き場が要る | glama の掲載ページ |

手元での実行 (2026-10-24): Claude Code の LSP ツールで、dotfiles の `src/glogx/action_modal.go` から `workspaceSymbol` を `Modal` で引いた。
gopls (`bin/gopls`) が **100 件で打ち切って**返し、あいまい一致なので `Modal` を含まない `advancePullAnim` なども混ざった。
sourcekit-lsp は `/usr/bin/sourcekit-lsp` にあるが、Swift の repo で Claude Code から引けるかは未確認。

## 分かったこと (暫定。下の実測で確かめる)

1. **名前で引く手段は LSP にある** (Claude Code の LSP ツール・Serena) が、言語サーバと最初の索引の待ちが要る。**LSP は使わないと決めた** (ユーザー決定 2026-10-24: dupscan だけで依存も使い方もシンプルになるなら LSP は使わない)。名前の検索も dupscan の索引 (897) でまかなう
2. **本体の形が同じものを後から見つける**部分は、CPD・jscpd で一部取れる可能性がある。ただし Swift で、名前・リテラルの違いと値の書き方の違い (L3) を潰すものは見つからなかった
3. **編集の直後に、近い既存のメソッドを自動で知らせる**組み込みを持つ道具は見つからなかった。jscpd の MCP と DRYwall は Claude から呼べるが、呼んだときにだけ動く。当初は編集の直後の hook (902) を中心にしたが、止める網は `make test` の `gate` (905) に変えた (902 は保留)。既存の道具を `gate` から呼ぶ形でも作れるので、dupscan のコアを自作するかは実測で決める
4. Go の重複チェックは dupl が既に回っている (7 module)。Go を dupscan に足す価値は、索引と `gate` (905) の側にある (900)

## 受け入れ条件

- [x] obaket の PR 24 の前の木 (commit 45cc3b208^) に CPD と jscpd を当て、6 つのうち何個が出るかを測る。既定の設定と、下限 (`--minimum-tokens` / `--min-tokens`) を下げた設定の両方。
      (jscpd は実測。CPD は Java が無く未実測: 下の「実測」)
      版・stdout / stderr / rc を分けて書き、obaket のあるマシンで行う
- [x] DualNoteApp の `macOS/` にも CPD と jscpd を当て、何が出るかと所要を書く (jscpd だけ。CPD は同上) (このマシンで今できる。調べる相手の repo に書き込まない置き場所で動かす)
- [x] ast-grep の Swift のパターンで、PR 24 の 6 つを形で引けるかを試す (直書きの 4 つと、プロパティを読む 2 つ)
- [x] 結果で、dupscan が自分で作る範囲を決めて [891](../891-design-dupscan-requirements.md) の「決めること」に書き戻す。既存の道具で依存と使い方が単純なまま足りるなら作らない (足すと依存と使い方が増えるので、その分も比べる)

## 実測 (2026-10-25。M4Pro・obaket の PR 24 の前の木 `4cb7cc740` の `shared/Tests` + `macOS/ObaketTests` 801 ファイル)

道具は作業用の置き場所の npm にだけ入れた (グローバルには入れていない): jscpd 4.3.0 (`jscpd@4`)・ast-grep 0.51.0。node v24.12.0。対象の repo には何も書いていない (終わった後の `git status --short` が空)。
「9 つ」は PR 24 の 6 つ (直書き 4・プロパティを読む 2) と、dupscan が同じ L1 の組に出した macOS のテストの写し 3 つ (obaket の issue 1301)。

**jscpd** (`--format swift`。「対」は clone の両側が 9 つのどれかに重なった数):

| 設定 | 既定のファイルの上限 | clone 数 | 9 つのうち対に入った数 | wall |
|---|---|---|---|---|
| 既定 (50 トークン / 5 行) | 既定 | 1009 | — (下の理由で 2360 行と 1425 行のファイルを読まない) | 21.2 秒 |
| 既定 | 上げた (`--max-lines 100000 --max-size 10mb`) | 2213 | 7 / 9 (4 組: Build–PathMapping・Reconcile–Resumable・Executor–Resumable・StorageBrowser–UploadDestination) | 17.0 秒 |
| `--min-tokens 20` | 上げた | 6745 | 6 / 9 | 15.7 秒 |
| `--min-tokens 10` | 上げた | 8663 | 3 / 9 (macOS の 2 組だけ。最長一致の clone に吸収される) | 13.7 秒 |
| `--min-tokens 10 --min-lines 3` | 上げた | 21389 | 3 / 9 | 18.0 秒 (既定の heap では OOM・rc=134) |

- **既定の `--max-lines 1000` / `--max-size 100kb` で、大きいテストファイル (`MoveReconcileExecutorTests.swift` 2360 行・`TransferRetryCoordinatorTests.swift` 1425 行) を黙って読み飛ばす** (stdout に出ない。報告の JSON にこの 2 ファイルが 1 件も出ないことで確かめた)
- 6 つを 1 つの組にした設定は無い。直書きとプロパティを読む版を結んだ clone は 0 (識別子・リテラルを潰さない。strict でも同じ)。1 行で書いた `MutationFinishing…` の版はどの設定でも対にならない
- DualNoteApp の `macOS/` (210 ファイル。上限を上げた設定): 既定で 93 clone (重複行 3.1%)・1.1 秒、`--min-tokens 10` で 682 clone。真偽は精査していない

**ast-grep** (`-l swift`): `StorageLocation(connectionID: $A, namespace: $B, path: ObjectPath(rawValue: $C))` の呼び出しを含み `-> StorageLocation` を返す関数の rule で **9 / 9** を引けた (98 件。関数の中でその呼び出しを使うだけのテスト関数が約 16 件混ざる)。0.25 秒。
形を先に知っている問い合わせで、似たものを組にする処理・類似度は無い (未知の重複は探せない)。

**PMD CPD**: 未実測 (このマシンに Java が無く、JDK を入れるのは環境を変えるので見送った)。公式文書 (pmd_userdocs_cpd) では `--ignore-literals` / `--ignore-identifiers` の対応言語に Swift が無く、名前・リテラルの違いは潰せない見込み (実測ではない)。
**再開の条件**: Java のある環境で、CPD が Swift で名前・リテラルを潰せるという情報が出たとき

## 決着 (2026-10-25)

既存の道具だけでは、dupscan の段ごとの組 (名前の違う写しを L1 で、直書きとプロパティを読む版を L3 で結ぶ) を置き換えられない。jscpd は同じ書き方の写しは出すが、上限で大きいファイルを黙って落とし、直書きとプロパティの版を結ばず、clone の数が多い。
ast-grep は形を知っている問い合わせには強いが、組にしない。**dupscan の正規化と組分けは自作のまま続ける** (891 の「決めること」に書き戻した)。依存も増やさない (jscpd は node、CPD は Java が要る)。

## 出典

- PMD CPD: https://docs.pmd-code.org/latest/pmd_userdocs_cpd.html
- jscpd: https://unpkg.com/jscpd@5.4.1/README.md
- dupl (golangci-lint): https://github.com/golangci/dupl
- mizchi/similarity: https://github.com/mizchi/similarity
- ast-grep の言語: https://docs.rs/ast-grep-language
- Serena: https://github.com/oraios/serena
- DRYwall: https://github.com/nikhaldi/drywall
- jscpd の mode の定義: https://unpkg.com/jscpd@4.0.5/README.md
- semantic-code-mcp: https://glama.ai/mcp/servers/@vrppaul/semantic-code-mcp

## 進捗

- 2026-10-24 起票 (ユーザー指示: 既存の道具の調査)。Web の一次情報と、手元の LSP ツールの実行で上の表を作った
- 2026-10-24 反証レビュー (事実の観点。読み取りのみのサブエージェント) の指摘を取り込んだ: dupl は 3 ではなく 7 module (tuikit はテストを除外しない) / jscpd の MCP・Agent Skills と DRYwall を足した / semantic-code-mcp は Swift 非対応と書けた / jscpd の mode の定義 / similarity の「AI エージェント向けと明言」は言い過ぎだった
- 2026-10-24 LSP を使わないことにしたので、LSP の実測と、索引を既存の道具で作る比較を受け入れ条件から外した
- 2026-10-24 範囲の決め方に「依存と使い方が単純なままか」を入れた (LSP を使わない決定と基準を揃える)
- 2026-10-24 DualNoteApp でも既存の道具を測る条件を足した (ユーザー 2026-10-24: repo に依存しない汎用的な作りにしたい。途中の検証には DualNoteApp の macOS を使ってよい)
- 2026-10-25 obaket の PR 24 の前の木と DualNoteApp で jscpd・ast-grep を実測し (sonnet のサブエージェント。主張は報告の JSON で裏を取った)、決着を書いた。CPD は Java が無く未実測
