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

子 issue 7 本 (1073〜1079) のうち 1 本完了 (1073 の土台。2026-11-13)。依存の順 (1073 → 1074 → 1075 → 1076 → 1077 → 1078 → 1079) に進める。

| issue | 内容 |
|---|---|
| [1073](1073-feat-yomiyasu-go-foundation.md) | 土台: module・Python の `re` を写す層・本家の出力を正解として記録する仕組み・CLI と CI の配線 |
| [1074](1074-feat-yomiyasu-go-markdown-visibility.md) | markdown_visibility (Markdown の見える字・保護する範囲・語の一覧) |
| [1075](1075-feat-yomiyasu-go-lint.md) | lint (40 規則) |
| [1076](1076-feat-yomiyasu-go-diff.md) | diff (推敲の前後を比べて意味のずれを出す) |
| [1077](1077-feat-yomiyasu-go-check.md) | check (検査の記録と最終稿の照合) |
| [1078](1078-feat-yomiyasu-go-dotfiles-integration.md) | hook と、dotfiles の ja-textlint・ja-textlint-on-edit・ja-writing への組み込み |
| [1079](1079-perf-yomiyasu-go-benchmark-loop.md) | 一通り終わった後に、ベンチマークの文章で計測して Go のソースを改善するループ (最大 10 周) |

この epic の起点は、yomiyasu の要点が ja-writing / ja-textlint にどこまで入っているかを照合した結果 (2026-11-12)。
要点 76 項目のうち、取り込み済みが 8・一部だけが 33・未取り込みが 35 だった。取り込む価値が高いのは、否定対比・擬人化・前置きと定型の結び・
直訳語の型・直す前後の比較の 5 つ。どれも Go の実装の lint・diff と、1078 で ja-writing に足す規範の文で扱う。
