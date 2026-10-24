# 901 (research): 既存の道具の調査 — dupscan を作る前に、使えるもの・足りないものを確かめる

起票日: 2026-10-24

## 概要

[891 (要件の正本)](891-design-dupscan-requirements.md) は、既存の道具を調べずに書かれていた (ユーザー 2026-10-24「既存ツールに関しては考慮不足」)。
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

- [ ] obaket の PR 24 の前の木 (commit 45cc3b208^) に CPD と jscpd を当て、6 つのうち何個が出るかを測る。既定の設定と、下限 (`--minimum-tokens` / `--min-tokens`) を下げた設定の両方。
      版・stdout / stderr / rc を分けて書き、obaket のあるマシンで行う
- [ ] ast-grep の Swift のパターンで、PR 24 の 6 つを形で引けるかを試す (直書きの 4 つと、プロパティを読む 2 つ)
- [ ] 結果で、dupscan が自分で作る範囲を決めて [891](891-design-dupscan-requirements.md) の「決めること」に書き戻す。既存の道具で依存と使い方が単純なまま足りるなら作らない (足すと依存と使い方が増えるので、その分も比べる)

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
