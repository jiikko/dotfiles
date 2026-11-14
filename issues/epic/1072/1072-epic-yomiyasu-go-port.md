# 1072 (epic): yomiyasu を Go で作り直す (src/ja-writing)

起票日: 2026-11-12

yomiyasu ([nanaism/yomiyasu](https://github.com/nanaism/yomiyasu) v3.1.0、MIT。AI が書いた日本語を推敲する Agent Skill) の Python の道具を、
本家と同じ答えを出す Go の実装として `src/ja-writing` に作り直し、`ja-textlint` と Markdown を書いた直後の hook から使う。

ユーザー決定 (2026-11-12): 作り直すのは全部 (lint・diff・check・hook・markdown_visibility) / Go の実装は `src/` の下に書く /
textlint に新しい規則は足さず Go の実装に寄せる / 答えは本家 (Python の実装) と揃える。時間はかけてよい。

名前 (ユーザー決定 2026-11-13): Go の道具は skill に揃えて `ja-writing` と呼ぶ (`src/ja-writing`・`bin/ja-writing`、`ja-writing lint | diff | check | hook`)。
dotfiles では skill・src の module・bin のコマンドを同じ名前にする通例 (zundamon-kaisetsu・slack-bbs) に合わせた。本家の写しであることは README と LICENSE に書く。
1073 の係が仮の名前 `src/yomiyasu` で作った分は、改名の commit で `src/ja-writing` にした (子 issue の表記も同じ commit で直した。残したのは、本家を指す名前と、本家と同じ答えを出すための名前: 本家のファイル名・`.yomiyasu-words`・`.yomiyasu`・`YOMIYASU_STATE_DIR`・oracle の cache の `yomiyasu-oracle`)。
入口のコマンドは `ja-textlint` のまま (textlint と `ja-writing lint` を回す。1078)。

## 状態

子 issue 12 本 (1073〜1084) のうち 8 本完了 (1073 の土台・1074 の markdown_visibility・1075 の lint・1080 の ja-writing の規範の文・1081 の diff の照合・1082 のベンチマークの下ごしらえ・1083 の lint の太字・1084 の lint の読点。2026-11-13)。
1075 では 40 規則が揃い、CLI の記録 8,072 件と dotfiles の文書 1,684 本で、出力と rc が全件本家と同じになった。
長い 1 文書の所要は本家とほぼ同じで、Go が速いのは起動込みの小さい文書だけ (1079 の before)。1076 (diff) は群 A・B・C・E・F (文の種類の印)・G (9 キーの組み立て)・H (許可・能力)・I (選ぶ範囲・因果)・J (全文差分)・K (見出しの構造)・L (文の組と本文)・R (語の一覧の読み)・Z (文体の硬化)・AA (勢い)・M (数値)・Q (概数と範囲の語)・N (否定)・O (主語と目的語の入れ替え) まで入り、`--endings` と引数の誤りが本家と同じで、全文差分と検査の本体へ渡す値も本家の記録と同じ (CLI の記録 8,002 件のうち 2,382 件と、本体へ渡す値 5,6xx 件、関数の記録と、全文差分の値を引数に取る段の記録 (群 K・L) を比べて通る。関数の記録は 71,290 case・312,340 呼び出し)。残りは人が読む出力 (群 AD)・P S T U V W X Y・AB・AC (issue 1076 の「進捗」)。依存の順 (1073 → 1074 → 1075 → 1076 → 1077 → 1078 → 1079) に進める。

| issue | 内容 |
|---|---|
| [1073](done/1073-feat-yomiyasu-go-foundation.md) | 土台: module・Python の `re` を写す層・本家の出力を正解として記録する仕組み・CLI と CI の配線 |
| [1074](done/1074-feat-yomiyasu-go-markdown-visibility.md) | markdown_visibility (Markdown の見える字・保護する範囲・語の一覧) |
| [1075](done/1075-feat-yomiyasu-go-lint.md) | lint (40 規則) |
| [1076](1076-feat-yomiyasu-go-diff.md) | diff (推敲の前後を比べて意味のずれを出す。群 A・B・C・E・F・G・H・I・J・K・L・R・Z・AA・M・Q・N・O まで) |
| [1077](1077-feat-yomiyasu-go-check.md) | check (検査の記録と最終稿の照合) |
| [1078](1078-feat-yomiyasu-go-dotfiles-integration.md) | hook と、dotfiles の ja-textlint・ja-textlint-on-edit・ja-writing への組み込み |
| [1080](done/1080-docs-ja-writing-yomiyasu-norms.md) | ja-writing の skill に yomiyasu の推敲の規範の文を足す (Go の実装と独立。dotfiles-24 が担当) |
| [1081](done/1081-feat-yomiyasu-go-difflib.md) | diff の照合 (`difflib.SequenceMatcher` と本家の速い照合) の写し (1076 から切り出した。dotfiles-bb が担当) |
| [1082](done/1082-perf-ja-writing-bench-corpus-baseline.md) | ベンチマークの文章を集め、本家の Python の所要を測る (1079 の前半を切り出した。dotfiles-24 が担当) |
| [1083](done/1083-feat-ja-writing-lint-bold-not-rendered.md) | lint の表示されない太字の群 G3 (bold_not_rendered。1075 から切り出した。dotfiles-24 が担当) |
| [1084](done/1084-feat-ja-writing-lint-pause-comma.md) | lint の読点の群 G13 (pause_comma。1075 から切り出した。dotfiles-bb が担当) |
| [1079](1079-perf-yomiyasu-go-benchmark-loop.md) | 一通り終わった後に、ベンチマークの文章で計測して Go のソースを改善するループ (最大 10 周) |

この epic の起点は、yomiyasu の要点が ja-writing / ja-textlint にどこまで入っているかを照合した結果 (2026-11-12)。
要点 76 項目のうち、取り込み済みが 8・一部だけが 33・未取り込みが 35 だった。取り込む価値が高いのは、否定対比・擬人化・前置きと定型の結び・
直訳語の型・直す前後の比較の 5 つ。どれも Go の実装の lint・diff と、1078 で ja-writing に足す規範の文で扱う。
