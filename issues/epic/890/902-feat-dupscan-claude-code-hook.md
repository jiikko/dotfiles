# 902 (feat): Claude Code が編集した直後に、近い既存のメソッドを知らせる hook

起票日: 2026-10-24

## 概要

epic 890 の主な用途 (ユーザー 2026-10-24): **Claude Code にコードを変更させている間に、重複したメソッドを作らせない・
既存のメソッドを見つけやすくして実装を滑らかにする**。[901](901-research-dupscan-existing-tools.md) の調査では、
編集の直後に自動で知らせる組み込みを持つ道具は見つからなかった (jscpd の MCP と DRYwall は、呼んだときにだけ動く)。

効かせる時点は 2 つある。この issue は 2 つ目を担当する:

1. **書く前**: 既存のメソッドを探す手順。名前・署名の型・doc 文で引く (dupscan の索引。[897](897-feat-dupscan-index-subcommand.md)。LSP は使わない) — 手順は [903](903-docs-dupscan-search-before-write-procedure.md) で先に書く。
   書く前には本体がまだ無いので、本体の形 (L1〜L3) で引くことはできない
2. **書いた直後 (この issue)**: PostToolUse の hook で、編集されたファイルの中の新しい・変わった単位を取り出し、dupscan の単位の一覧 (本体の形のハッシュを持つキャッシュ。1 行 1 件の索引の Markdown ではない) と本体の形 (Type-2・L1〜L3) で比べる。
   近いものがあれば、名前・file:line・一致した段を返す。既定は止めずに知らせるだけ

## 要件

- **返す経路**: `hookSpecificOutput.additionalContext` で返す (exit 0 のまま stdout に出しても Claude には届かない。dotfiles の既存の hook も
  additionalContext で返している。例: `_claude/hooks/git-state-verify.sh`)。additionalContext は背景情報として届き、指示ほど強くない
  (dotfiles の `CLAUDE.md`「`_claude/` を触るとき」)。そのため、書く前の手順 (903) と組にする
- **新しい・変わった単位の取り出し**: 編集後のファイルを parse し、索引の同じファイルの単位と、鍵 (ファイル + 名前・署名) で突き合わせて、新規と変更を決める。
  行はずれるので file:line は鍵にしない。**自分自身の旧版 (索引に残っている編集前の同じ関数) を一致から除く**
- **見る道具**: Edit / Write / MultiEdit を matcher にする。**Bash (sed・python など) で書かれた変更と NotebookEdit は見ない**。これは「検出しない形」として README に書く
- **索引の鮮度と置き場所**: 索引は worktree ごとに置き、hook は編集したファイルの分だけ差し替える。誰がいつ作り直すか、複数のセッション・hook が同時に書いたときの扱い
  (原子的な置き換え・lock) を決める。commit して置く索引 (897 の `--check`) と、hook が使うキャッシュの関係もここで決める
- **所要**: 編集のたびに走るので、1 回の上限を決めて測る (未実測)。`bin/dupscan` の初回ビルド (cgo。[892](892-chore-dupscan-project-scaffold.md)) を hook の中で走らせない。
  実行ファイルが無ければビルドを促す 1 行だけを返す
- **知らせる量**: 既存の単位の名前と署名・file:line・可視性・doc comment の 1 行目・一致した段。呼び出し元の数など近似の値には、近似の印を付ける ([896](896-feat-dupscan-report-output.md) と同じ表し方)。共通の置き場所にあるものは「これを使う」、
  ファイルの中の private なものは「寄せる候補」と区別する。件数に上限を置く
- **誤検知**: 引数を取らない単位は L3 で本体全体が 1 記号に畳まれ、同じ型を返す関数がどれも一致しうる ([891](891-design-dupscan-requirements.md) の L3)。
  hook で知らせる段と下限は、[898](898-test-dupscan-obaket-calibration.md) の否定の期待値を通ったものだけにする
- **黙らない・うるさくしない**: 対応する言語の拡張子 (最初は `.swift`) のファイルの編集にだけ反応する。索引が無い・読めない構文で確かめられなかったときは
  「確認できなかった」と返すが、同じ理由ならセッションで 1 回にする。沈黙を「重複なし」に読ませない
- **opt-in**: 使う側の repo が settings の hook で有効にする。置き場所 (dotfiles の `_claude/hooks/` か、使う側の repo か) は [891](891-design-dupscan-requirements.md) の「決めること」

## 受け入れ条件

- [ ] obaket の PR 24 の前の木で、6 つのうち 1 つを消してから Edit で書き直したとき、hook が残りの 5 つ (少なくとも共通の置き場所のもの) を返す
- [ ] 既存の関数の中身だけを変えた編集で、その関数自身の旧版を返さない
- [ ] 無関係な編集・対応していない拡張子の編集で何も返さない (否定の期待値)
- [ ] 1 回の hook の所要を obaket の規模で測って書く
- [ ] 検出しない形 (Bash での書き換え・NotebookEdit) を README に書く

## 依存

- [893](893-feat-dupscan-core-normalize-type2.md)・[894](894-feat-dupscan-swift-adapter.md)・[897](897-feat-dupscan-index-subcommand.md) (同じ単位の一覧を使う)
- [898](898-test-dupscan-obaket-calibration.md) (知らせる段と下限)
- [901](901-research-dupscan-existing-tools.md) の結論 (既存の道具を hook から呼ぶ形で依存と使い方が単純なまま足りるなら、そちらを使う)

## 進捗

- 2026-10-24 起票 (ユーザー指示: 主な用途は Claude にコードを変更させている間の重複の防止と、既存のメソッドの発見性)
- 2026-10-24 反証レビュー (設計の観点。読み取りのみのサブエージェント) の指摘を取り込んで書き直した: 書く前は本体の形で引けない (署名の型と doc で引く。手順は 903 へ分けた) /
  返す経路は additionalContext / 新しい・変わった単位の取り出しと自分自身の旧版の除外 / 見ない道具 / 索引の鮮度と並行 / 初回ビルドを hook で走らせない / 898 への依存 / 拡張子と「確認できなかった」の回数
- 2026-10-24 反証レビュー (LSP を使わない改訂): 比べる相手を単位の一覧のキャッシュ (形のハッシュを持つ) と明記し、近似の印を足した
