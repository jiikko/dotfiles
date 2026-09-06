# 161 retro: ~/.claude link 漏れの自動修復 (issue 160) — 2026-09-02

起票日: 2026-09-02
種別: retro
関連: [160](160-claude-link-leak-after-session-ends.md) / commit 9cc51ee

## 反省・気づき

1. **変異検証で「意図と違う変異」を当てて red を読んでいた**。`perl -0pi -e "s/.../.../"` を
   二重引用符で書いたため、置換側の `$ROOT` / `$link` がシェルに展開されて空になり、
   「-ef 不一致を見ない」のつもりが「常に drift 扱い」に、「*.js フィルタを外す」のつもりが
   「workflows のパスを壊す」になっていた。red は出たので気づかず、件数 (8 件のはずが 6 件) の
   不整合で初めて発覚。`mutation-verify-new-tests.md` は「ビルドできたか」を確認させるが、
   **「当たった変異が意図したものか (diff を見る)」までは要求していない**。
   → 切り出し先: `_claude/rules/mutation-verify-new-tests.md` の手順 1.5 に
   「変異の diff を目視し、意図した箇所だけが変わっていることを確認する」を 1 行足す
2. **敵対レビューは想定の外側を出した (実測 6 回目)**。`ln -sfn` の dest が実ディレクトリだと
   `-n` が効かず中へ潜り込む、という挙動は私の変異 8 本のどれも試していなかった。守っていたのは
   実ファイル用に書いたガード 1 行の副作用。→ 切り出し不要 (`adversarial-review-own-safeguards.md`
   の「変異は本ルールの代わりにならない」の実例として、あちらの実測回数を 5 → 6 に更新するだけ)
3. **未実測が 1 つ残っている**: SessionStart hook が張った rule が**その**セッションで読まれるか。
   hook の報告は「遅くとも次のセッションから」と保守的に書いた。測り方は簡単で、rule を 1 本足した
   直後のセッションで hook の「補った」報告が出たとき、その rule の本文が context に在るかを見る。
   → 切り出し先: human issue にはしない (次に rule を足すセッションが自然に観測できる)。
   観測できたら `claude-links-sync.sh` のヘッダ注記と issue 160 の「未実測」を書き換える
4. **テストの期待件数を数え間違えた (8 と 7)**。fixture を書いた直後に手で数えるより、`list` の
   出力を一度目で見て件数を確定してから assert を書けばよかった。→ 却下 (一般化するほどではない)
5. **`make test` 全体を回さずに commit し、CI を赤にした** (9cc51ee / 1592682、`tests/setup/test_setup.sh`)。
   並行セッションが Go の rename を進めていたので「無関係な赤が混ざる」と考え tests/claude だけ回した。
   しかし setup.sh を触った変更なのに tests/setup を回していない。memory の「コミット前に make test」を
   状況判断で外したのが直接の原因。回すコストは 3 分、赤の切り分けはターゲット名で足りた。
   → 切り出し先: 却下 (既にルール化されている。守らなかっただけ)。ただし補助として次項
6. ~~`scripts/test_changed.sh` に `setup.sh` の写像が無い~~ **誤り**。`grep setup scripts/test_changed.sh`
   が空だったことを「写像が無い」と読んだが、`*.sh` の汎用写像 + `add_test_dirs_referencing` が拾う。
   実測 (2026-09-02): `make test-changed PATHS=setup.sh DRY_RUN=1` → `tests: tests/claude tests/setup
   tests/zshrc`。使っていれば tests/setup は回っていた。問題は写像ではなく、私が test-changed も
   `make test` も使わなかったこと (項目 5)。→ 却下。不在の主張を grep 1 回で断定した点は
   CLAUDE.md「ぼやきも事実の主張なら裏を取る」の再演
7. **1 周目の敵対レビューは commit 前のコードにしか当たっていなかった**。`adversarial-review-own-safeguards.md`
   の節 7 (指摘を直した差分にもう 1 周) は「1 周目の指摘への修正」を対象にしているが、今回 2 周目で
   出た P1 2 件 (setup.sh の回帰 / ln -sfn の並行) は 1 周目の観点 (壊す / 素通り) にそもそも無かった
   「回帰」「並行」の観点から出た。観点の選び方の問題で、節 5 の最低限の分け方には「並行・中断」が
   既に入っている。私は 1 周目でそれを省いた。→ 切り出し不要 (節 5 を守るだけ)
8. **commit message に不正確な主張を 2 つ書いた** (「fork 無し」「変異 9 本」)。書く前に `$(...)` と
   `<(...)` を数えれば分かったこと、変異の本数は当てた list を数え直せば分かったこと。
   → 切り出し先: 却下 (`perf-claims-need-measurement.md` の範囲。issue 160 に訂正を残した)

## 決着 (2026-09-02)

- 項目 1: `mutation-verify-new-tests.md` に手順 1.6 (変異の diff を目視) を追加。起源は rules-rationale へ
- 項目 2: `adversarial-review-own-safeguards.md` / `mutation-verify-new-tests.md` の実測回数を 5 → 6
- 項目 6: 実測で写像が在ることを確認し却下 (上記)
- 項目 3, 4, 5, 7, 8: 却下 / 切り出し不要 (各項目に理由)

残課題なし。
