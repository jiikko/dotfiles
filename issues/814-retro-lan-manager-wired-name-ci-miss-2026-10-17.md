# 814 (retro): lan-manager の有線の名前〜一覧の印のセッション (CI の赤を 4 push 見落とした)

起票日: 2026-10-17

対象: 2026-10-17 の 809 の後の作業 (747 の有線の名前・一覧で名前を使う・整合の監査・`O` のコピー・初期設定の印、CLAUDE.md / README の一括点検)。
809 (固定 IP のデバッグ) の続き。

## どこで踏んだか

### 1. push した後に CI を見ず、src/glogx の赤を 4 push 続けて見落とした (ユーザーが見つけた)

- 98002fc9 (有線の名前の広告) から src/glogx の no-real-commands が毎回落ちていた。glogx のテストの L の偽物 (`fakeLanOptions`) が
  `live.Options()` から危ない口を 1 つずつ外す形で、新しく足した `ReadAdvert` (launchctl) を外していなかった
- 手元の `make test` は緑だった。手元の no-real-commands は一部のテスト (`-run 'TestUpdateKeys|TestDoctor|…'`) しか回さず、
  `TestLan*` は CI の `GLOGX_SHIM_SCOPE=all` でだけ走る。手元の緑は CI の緑を予言しない形だった
- 各 push の後に `ci-log` を 1 回打っていれば、最初の push で見えていた (`.claude/rules/use-ci-log-for-ci-inspection.md` は
  「名指しで待った run が緑でも引数なしの ci-log を打つ」と書くが、そもそも待たずに次へ進んだ)

### 2. テストの偽物を「本物から危ない口を外す」形にしていた

- 外す形 (deny-list) は、本物の側に口が増えるたびに外し忘れが本物を起こす。今回は `ReadAdvert` のほか、`Observe` (ssh)・`LookupHost`
  (dscacheutil)・`Copy` (pbcopy)・`RunQuiet` も本物のまま残っていた (検出側の shim が見ていないので CI でも出なかった)
- 直し方は「空から組み、安全な口だけ本物から写す」(allow-list)。口が増えても空 = 何もしない、になる

### 3. 空いていないキーを「空いている」と推した

- `O` のキーを決めるとき、`f` を「空いているキー」として推奨にした。`f` は `tuikit/listnav` の半ページの移動で、ガイドも
  「動作キーに移動の語彙を使わない」と書いていた。実装に入ってから気づき、選び直してもらった (往復 1 回)

### 4. 読み取りのみのサブエージェントが /tmp に書いた

- 照合役 (sonnet) が調査コマンドに `> /tmp/x` を入れた。指示は「何も編集しない」だけで、リダイレクトを名指ししていなかった。
  次の組からは「/tmp を含むどこにも書かない — 出力のリダイレクトもしない」と書き、以後は出なかった

## 提案

1. **手元の検査が CI より狭い範囲しか回さない (scope の切り替え・`-run` の絞り込み) と知っている変更では、push の後に CI の結果を待ってから
   次の push・完了報告へ進む。待たないなら CI の範囲を手元で 1 回回す**。手元の緑は「手元の範囲」の緑でしかない
   - 切り出し先: `_claude/rules/verify-execution-not-just-exit-code.md` の「最後の編集の後に、変更したファイルを実際に検査する gate を回す」の項へ追記
2. **本物の依存の束 (Options・config・client) からテストの偽物を作るときは、ゼロ値から組み、安全と確かめた口だけを本物から写す。
   本物を丸ごと受け取って危ない口を外す形にしない** (束に口が増えるたびに、外し忘れが本物を起こす)
   - 切り出し先: `_claude/rules-checklists/mutation-verify-new-tests.md` の fake / stub の節へ追記
3. 「空いているキー / 名前 / 番号」を選択肢として出すのは不在の主張なので、出す前に定義側 (キーの割り当て・名前の一覧) を読む
   - 却下: `~/.claude/CLAUDE.md`「ぼやきも事実の主張なら裏を取る (特に不在の主張)」と同じ規範で、新しく書き足すことが無い
4. 読み取りのみの指示に「リダイレクトでどこかへ書く」を名指しで入れる
   - 却下: `_claude/rules/subagent-model-tiering.md`「指示を書くとき」の「道具が黙って書く副作用も名指しで止める」で足りる。今回はそれを
     ファイルの編集だけに読んで書いた (適用の漏れで、規範の欠けではない)

## 局所 (その場で直した / 直さない)

- `fakeLanOptions` は allow-list に直した (bbde1270)。no-real-commands の shim が ssh・dscacheutil・dns-sd・pbcopy を見ていない件は
  issue 747 の進捗に「未対応」として書いた (issue 化はユーザーの判断待ち)
