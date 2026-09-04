# retro: glogx 起動の律速調査・演出 2 倍速・epic group 対応の見直し

起票日: 2026-09-05

## 踏んだところ

1. **n=2 の観測を採用しかけた**。「spawn 直後に kill -9 で温まる」が 2/2 で出て採用寸前だった。
   n=10 で 0/10 (kill が競合で外れて走り切っていたときだけ効いていた)。
   → 切り出し: **却下** (既存 `adversarial-review-own-safeguards.md` §1「異常系を実験で作る」と
   `verify-execution-not-just-exit-code.md`「有無で結果が変わらない観測を証拠に数えない」で足りる。
   足すなら後者に「n が小さい A-B は非決定性を見ていない」を 1 行。今回は見送り)
2. **非 TTY 経路の同期 `gh` 呼び出しを popup の起動コストに混ぜて報告した**。`--no-pager` で
   測った 1.30s のうち大半は `fetchStatic` (TTY では走らない) で、ユーザーへ訂正が要った。
   → 切り出し: **既存へ追記候補** `measure-external-cli-streams-separately.md` に
   「計測経路がユーザーの実経路 (TTY / 非 TTY、対話 / パイプ) と同じかを先に確かめる」。判断待ち
3. **変異をテストの実行経路の外に当てた** (ビルド失敗テストに対して mv 失敗分岐を変異し、
   green のまま「守れている」と読みかけた)。経路上へ当て直して red。
   → 切り出し: **却下** (別マシンが同日 `mutation-verify-new-tests.md` 手順 1.6 に
   「その行が対象テストの実行経路上にあることを確認する」を足しており、既に規範化済み)
4. **変異を当てた状態の tree で `make test` を background 起動した** (2 つの Bash を並列に出し、
   片方が変異→復元、片方が make test)。結果は復元後の版でコンパイルされていた (glogx/issues が
   ok で、変異版なら落ちる) が、規律違反で運が良かっただけ。
   → 切り出し: **却下** (`mutation-verify-new-tests.md`「共有 working tree で変異を当てない /
   変異中は一切 commit しない」と `parallel-write-agents-need-worktree-isolation.md` の
   「レビュー中に同じファイルを書き換えない」で既に禁止。守り方の問題)
5. **別マシンの epic 対応で、テスト側の depth 上限 (`-maxdepth 2`) が新しい階層を黙って外していた**。
   spec / rule / hook / viewer は揃っていたのに、検査 1 本と README だけが旧前提のままだった。
   → 切り出し: **既存へ追記候補** `new-tool-requires-entrypoint-docs.md` か `claude-md-maintenance.md` に
   「ディレクトリ階層の契約を変えたら、`find -maxdepth` / `ls <dir列挙>` のような
   **深さや置き場所を前提にした検査・スニペット**を grep する」。判断待ち

## 残課題

- 項目 2 と 5 の追記先をどうするか (ユーザー判断)
