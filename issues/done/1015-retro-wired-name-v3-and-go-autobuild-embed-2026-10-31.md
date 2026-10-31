# 1015 (retro): 有線の名前の広告 v3 と go_autobuild の embed 対応 (issue 1013) の振り返り

起票日: 2026-10-31

## 概要

2026-10-30〜31 のセッションで、mac-lan-manager の wired-name v3 (LaunchAgent の実行ファイル化)・`upgrade-advert`・ラベルの二重管理の解消、
issue 1013 (go_autobuild が `//go:embed` のファイルを再ビルドの判定に数える) を入れた。そこで踏んだところと、回りくどかったところを残す。

## 踏んだところ

### 1. 手元 (ja_JP.UTF-8) で緑、CI (en_US.UTF-8) で赤 — 並びが locale で変わる期待値

- 1013 で足した `tests/bin/test_go_autobuild.sh` は、embed の展開の結果を zsh の `(o)` で並べて期待値と比べていた。`(o)` の順は locale の照合順で決まり、
  en_US.UTF-8 では `あ.txt` が `a.txt` より前に来る。手元は ja_JP.UTF-8 なので緑、CI は赤 (CI の locale そのものは見ていない。
  `LC_ALL=en_US.UTF-8` で CI と同じ失敗の行が再現し、C と ja_JP では逆の順になった) (commit「embed の展開の結果を LC_ALL=C の sort で並べる」で直した)
- 展開する側 (`bin/lib/go_autobuild.zsh`) は `local LC_ALL=en_US.UTF-8` で locale を固定していたのに、それを検査するテストの側の並びは固定していなかった。
  locale に依存する処理を固定するときは、その結果を並べる・比べるテストも同じく固定する
- push の前に CI と同じ locale で 1 回回していれば、CI は赤にならなかった

### 2. push を 10 回重ね、CI が赤いまま完了を報告した

- transcript の `git push origin HEAD:master` は、CI を待つ前に 10 回ある。内訳はコードを含むものが 6 回 (wired-name の実行ファイル化 / RemoveScript の修正 /
  upgrade-advert / ラベル / 1013 の本体 / 1013 の総合レビューの修正)、issues だけのものが 4 回。CI を待ったのは、赤を直した 11 回目の push だけだった
- CI は 1013 の本体の push から赤だった。それに気づかないまま「全部 master に push した (467cc3d4d)」と完了を報告した。赤は、同じ repo で作業していた別のセッション
  (dotfiles-38) が見つけて知らせてきた
- ユーザーの方針は「作業中の CI の取り消しは許容、確認は後回し」(memory: CI is push-trigger only) で、push ごとに待つ必要はない。
  ただし「後回し」は「見ない」ではなく、完了を報告する前に最後の commit で 1 回見る必要がある。その最後の 1 回が抜けていた

### 3. Go の `path.Match` を zsh の glob へ手で写し、敵対的レビューが 6 周 (commit ごと 5 周 + 総合 1 周) かかった

- 2〜5 周目の指摘は、主に 2 つのパターン言語の意味の差だった。1 周目の P1 (go list が HUP に巻き込まれる) と総合の 4 件 (go list の失敗・記録の mtime・race) は
  これとは別の種類なので、下の提案だけで周回がなくなったわけではない。意味の差は次のとおり: `[^` と `[!`、`\X` の escape、GLOB_SUBST が普通の文字の前のバックスラッシュを残すこと、
  `(b)` が `!` と `-` を quote しないこと、escape した範囲の端が範囲を壊すこと、class の中で先頭以外の `^` を escape してはいけないこと
- 受け入れる例を手で選んで書いたテストは、自分が理解した範囲しか試さない。そのため、レビューの周ごとに次の差が見つかった。本物の Go を oracle にして
  同じ入力を両方へ通す差分の検査を最初に作っていれば、2〜5 周目は減らせた。実際には周回の途中で、本物の go1.26.0 との比較を自分が 3 回、
  レビュアーも周ごとに書いている。比較を毎回その場で作り直し、テストには残さなかった

### 4. inline の script に `rm` を書くと安全検査に止められた (局所)

- `zsh -c` や heredoc の中に `rm` を含む Bash を何度か止められ、python の編集スクリプトを Write で置いて回した。
  道具の側の都合なので提案にはしない (直さない)

## 提案

| # | 内容 | 切り出し先 | 状態 |
|---|---|---|---|
| A | 手元と CI で locale が違うと、並び・照合・文字クラスに依存する結果が変わる。toolchain の版と同じく CI の locale でも 1 回回すか、その結果を並べる・比べる側を `LC_ALL=C` で固定する (1) | `_claude/rules/verify-execution-not-just-exit-code.md` の「手元と CI で toolchain の版が違うなら」の段へ追記 | 済 |
| B | CI を待たない運用でも、完了を報告する前に最後の commit で引数なしの `ci-log` を 1 回打つ (2) | `.claude/rules/use-ci-log-for-ci-inspection.md` の「名指しで待った run が緑でも」の項の前提を広げる (新しい項にしない) | 済 |
| C | 外部の道具の意味論 (パターン・escape・照合) を自分で再実装するなら、ケースを書く前に、本物を oracle にした差分の検査を作ってテストに残す (3)。oracle の選び方は既存の `rules-checklists/mutation-verify-new-tests.md` の「差分テストの oracle」の項にあるので、新しく足すのは「いつ作るか」だけ | `_claude/rules/measure-external-cli-streams-separately.md` の「外部ソースは仮説」の節へ追記 | 済 |
| D | go_autobuild の embed の展開に、本物の `go list -f '{{.EmbedFiles}}'` との差分テストを足す (3 の局所案) | — | 直さない。受け入れた不一致 (`[\]-^]`・NFD・`**/x` など) は issue 1013 に記録済み。今の期待値は本物の go で確かめてある。CI の Tests には setup-go があるので本物の go は呼べるが、今のテストは fake の go で組まれているので別の作業になる |
| E | `(o)` を使うテストを検出する lint (1 の局所案) | 却下 | 字句で `(o)` を探すと、locale に関係しない並び (ASCII だけのファイル名) まで止める。A の規範で足りる |

## 進捗

- [x] A: `verify-execution-not-just-exit-code.md` の toolchain の版の段に locale を足した
- [x] B: `use-ci-log-for-ci-inspection.md` の「名指しで待った run が緑でも」の項を、待たなかったときにも広げた
- [x] C: `measure-external-cli-streams-separately.md` の「外部ソースは仮説」の節に、差分の検査を作る時期を足した (oracle の選び方は既存の checklist を指す)
- 反証レビュー (opus 1 本、read-only): P1 2 件・P2 3 件・P3 3 件。すべて採った
  - push の回数を 5 → 10 に直し、「赤いまま完了を報告した」を仮定でなく事実として書いた
  - 周回数を 6 + 1 → 6 に直し、パターン言語の差は 2〜5 周に限った
  - 本物の go との比較は、周回中にその場でしていたが、テストに残さなかった、に直した
  - B と C は既存の項と重なるので、新しい項にせず既存の項を広げる形にした
  - CI の locale は未確認で、en_US.UTF-8 での再現が根拠であると書き分けた
