# 953 (retro): dupscan の sweep・節 3 の読み・性能・A1 の観測 (2026-10-28)

起票日: 2026-10-28

このセッションでやったこと: 946 の残り (`sweep`・打ち切りの通知・report の注記)、claude-md-refresh の一括点検 (40 ディレクトリ)、`dupscan-dogfood` skill、節 3 の上位 7 組を読んで寄せる / 閉じる、リソースと確保の読み (951)、A1 を本物の新規セッションで観測。f5 と手分けして 6 module に dupscan を回した。

## どこで踏んだか / 回りくどかったか

- **利用者に約束する文面を、実機で通さずに書いた**: `sweep` の先頭に「`dupscan:allow` で静かになる」と書いたが、`sweep` は許可印を見ておらず偽だった (`HandleKey` に印を付けて初めて気づいた)。直前に別の出力 (`gate`) で「許可済みは quiet」と読んだことから、同じだと思い込んだ。
- **ホットループの内側に、測らずに判定を足した**: その許可印の判定 (`ex.Allowed`) を節 3 の対の総当たりの内側で毎回呼び、`sweep` が 5.0 → 7.5 秒、確保が 365 MB → 4.5 GB になった。プロファイルを取って初めて気づいた。足す前に「この呼び出しは何回走るか」を数えていなかった。
- **テストの偽物が、検索の向きを模さなかった**: A1 の hook のテストは、引いた語に応じた行を返す偽の dupscan だった。`index -name X` は「既存の名前が X を**含む**」ものを返すので、新しい名前より短い既存の名前は引けない。偽物にその向きが無く、f5 が実機で見つけるまで緑のままだった。
- **難しさをコードを読む前に断定した**: Go の AST 変換の確保を、プロファイルの関数名だけで「構造に触る」と見送った。読むと、機械的に `w.` を付けて arena に寄せるだけで済んだ。
- **変異が compile できないまま「緑」と読みかけた**: arena の変異で `declared and not used` により build 失敗 (`NOCOMPILE`)。既存の規約どおり第 3 の結果として当て直した。
- **計測の選び方**: `claude -p --output-format stream-json` の出力に、PreToolUse の注入が載らない。最初は「注入されていない」と読みかけ、transcript の jsonl を読んで届いていると分かった。
- **時間の測定が他セッションの負荷で揺れた** (load average 7)。確保回数・確保量は揺れないので、そちらを主にした。前後の版を交互に走らせると揺れを打ち消せた。
- **読んだ結果の収穫は低かった**: 節 3 の上位 7 組を読んで寄せたのは 2 組 (どちらも同じ module の test helper)。節 2 は tuikit・zundamon の 6 組が全部雑音。出た数を成果にしない、は 948 に記録した。

## 次に効きそうな改善 (提案。実行はユーザーの判断を待つ)

1. (反映済み: `verify-execution-not-just-exit-code.md` へ 1 文) **利用者 (人・agent) に「こうすれば〜になる」と約束する出力文は、書く前にその経路を実機で 1 回通す**。切り出し先: 既存 `_claude/rules/verify-execution-not-just-exit-code.md` へ 1 文 (「出力の案内文が約束する操作は、約束を書いた同じ変更の中で実機で通す」。ツールの `--help`・usage・エラーの案内も同じ)。新規ルールは立てない (発動点が既存の「走った証拠」の項と同じ)。
2. (反映済み: `perf-claims-need-measurement.md` へ 1 文) **既に測って決めた経路 (総当たり・毎行・毎ノード) の内側に判定や呼び出しを足すときは、足す前に実行回数を数え、足した後に前後を測る**。切り出し先: 既存 `_claude/rules/perf-claims-need-measurement.md` へ 1 文 (「ホットループに足す呼び出しは、1 回の費用でなく『内側での実行回数 × 費用』を見積もってから足す」)。
3. (反映済み: `survey-receiver-guards-before-passing-new-values.md` へ 1 文) **テストの偽物 (fake / stub) が検索・照合を模すなら、照合の向き (含む / 含まれる / 前方 / 完全) を受け側の実装から写す**。切り出し先: 既存 `_claude/rules/survey-receiver-guards-before-passing-new-values.md` の「テストの fake / stub に、受け側の reject 挙動を模させる」の項へ 1 文 (reject だけでなく、**照合の向き**も)。
4. (反映済み: `.claude/rules/worktree-per-session.md` へ 1 文) **headless の新規セッションで hook の注入を観測するときは、stream-json の出力ではなく、transcript (`~/.claude/projects/<project>/<session>.jsonl`) の `hook_additional_context` を読む**。切り出し先: `.claude/rules/worktree-per-session.md` の「push + pull の後は headless の新規セッションで観測する」の項へ 1 文。
5. (却下: 言い間違いの話で、既存の「ぼやきも事実の主張なら裏を取る」で足りる) **「難しい / 構造に触る」の見積もりは、コードを 1 回読んでから言う** (関数名やプロファイルの行だけで断定しない)。切り出し先: 却下候補 (局所的な言い間違いで、既存の「ぼやきも事実の主張なら裏を取る」で足りる)。

## 却下

- 変異が compile できない件: `mutation-verify-new-tests.md` の手順 1.5 に既出。守れた事例。
- 時間の測定が揺れた件: 交互に走らせる、確保回数を主にする、は今回の issue 951 に記録済み。一般化するほどの新しさは無い (`avoid-wall-clock-assertions.md` と `perf-claims-need-measurement.md` の範囲)。
