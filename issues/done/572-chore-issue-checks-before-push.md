# 572 (chore): issue の整合検査 (番号の一意性・相対リンク) を push の前に止める

起票日: 2026-09-28

## 概要

2026-09-27 夜から 09-28 朝まで、master の Tests が issue の整合検査 2 本で赤いままだった
(run 36358929358。7eb0b235 で解消):

- `tests/issues/test_issue_numbers_unique.sh`: 番号 563 が 2 件
  (`issues/pending/563-bug-nvim-plugin-checkout-drifts-from-lazy-lock.md` と、epic/415 の control plane の issue。後者を 571 へ改番した)
- `tests/issues/test_issue_links_valid.sh`: `issues/epic/415/pending/` の 554 / 561 の親リンクに `../` が無い

どちらも**手元で 4 秒以内に分かる** (2 本合わせて 3.7 秒、2026-09-28 実測) のに push を止めるものが無く、
しかも**気づいたセッションが「無関係の失敗」と書いたまま push を続けていた**:

- `issues/done/414-docs-scope-global-rules-with-paths-under-150k.md` の「残る 2 本は今回の変更と無関係で、master でも同じように落ちる」
- `issues/epic/415/497-feat-pro-con-delete-done-cards-after-a-week.md` の「落ちたのは今回と無関係の 2 本」
- `issues/epic/415/done/562-ux-pro-con-card-list-age-label.md` の「この変更とは無関係」

赤いまま他の作業が積み重なり、master の赤で別の退行が隠れる期間ができていた
(`verify-execution-not-just-exit-code.md` の「集約テストが赤いまま、その上で検証を続けない」に当たる)。

## 詳細 — 何が入口だったか

| 失敗 | 入り込んだ commit | 入口 |
|---|---|---|
| 563 の重複 | `ab460c8a` (09-27 19:58、nvim 側を 563 に採番) の後に `563c3e19` (22:30、control plane) | `563c3e19` の author email は `jiikko@users.noreply.github.com` で、他の commit (`n905i.1214@gmail.com`) と違う。**このマシンの hook を通らない環境 (GitHub の web や別の環境) から入った可能性がある** (未確認) |
| 554 / 561 のリンク | `17b8ad64` (554 を pending へ移す) / `74afcbaa` (561) | pending への移動を手で行い、相対リンクを張り直していない。移動とリンクの張り直しを 1 コマンドでやるのは `scripts/issue_done.sh` だけで、**移動先は常に `done/`** (`pending` / `waiting` / `next` の分岐は移動元から group を決めるためのもの)。**pending / waiting / next 行きの移動には無い** |

今ある hook: `githooks/pre-commit` (civility-lint だけ。`setup.sh` が `core.hooksPath githooks` を設定する)。**pre-push は無い**。

## 対応方針 (案。決めるのは着手時)

1. **`githooks/pre-push` を足す**: push する範囲 (stdin の `<local sha> <remote sha>`) が `issues/` を触っているときだけ
   `tests/issues/` の軽い検査 (少なくとも上の 2 本) を回し、落ちたら push を止める。
   - 🚨 **止めるのは「この push で新しく壊した」ときに限るか、既に壊れていても止めるか**を決める。
     既に壊れていても止めると、無関係の push も全部止まるので、直す動機にはなるが他のセッションの作業を止める
     (今回の形なら止めた方がよかった)
   - 失敗時の案内には直し方 (改番の手順は `issues/README.md` の採番節、リンクは検査の出力) を出す
2. **pending / waiting / next への移動も `issue_done.sh` と同じ仕組みに寄せる** (移動先を引数に取る等)。
   リンクの張り直しを人が覚えている必要がなくなる。1 だけだと「止まってから直す」往復が残る
3. hook を通らない入口 (web・別の環境・`--no-verify`) は hook では止められない。そこは CI が最後の網で、
   **CI の赤を「無関係」として積み増さない**という規律側 (上の rules) で受ける

## 受け入れ条件

- [x] `issues/` を触る push で、番号の重複 / 相対リンク切れがあると push が止まる (fixture で番号を重複させて確かめる)
- [x] `issues/` を触らない push では検査を回さない (待ち時間を増やさない)
- [x] 検査が走ったことが出力に出る (skip も skip と出る)
- [x] pre-push を足したことを `README.md` の githooks の節に書く
- [x] 対応方針 2 (pending / waiting / next 行きの移動もリンクの張り直しと 1 組にする) — 見送り (下の残タスク)

## 関連ファイル

- `githooks/pre-commit` / `setup.sh` (`core.hooksPath`)
- `tests/issues/test_issue_numbers_unique.sh` / `tests/issues/test_issue_links_valid.sh`
- `scripts/issue_done.sh`
- `issues/README.md` の「採番」節

## 進捗

- 2026-09-28: 起票。反証レビュー (sonnet 1 体、読み取り専用): 事実の主張はすべて裏が取れた (commit・author・時刻・引用・「pre-push は無い」)。P3 1 件 (issue_done.sh の分岐は移動元の判定であり、移動先ではない。書き方を明確にした) を反映
- 2026-09-28: 対応方針 1 を実装 (commit「githooks: issues/ を触る push で issue の整合検査を回して止める pre-push を足す (572)」)
  - `githooks/pre-push`: push する commit を一時 index に読んで書き出し、`tests/issues/` の 6 本を回す。
    issues/ を触る push では全部、issues/ の外の削除・移動だけの push ではリンク系 2 本だけ。どちらも無い push は省略と出して通す。
    「壊したのが今回でなくても止める」を選んだ (issue を触る push に限る。無関係の push は止めない)
  - `tests/githooks/test_pre_push.sh` (13 ケース、約 50 秒): repo の HEAD のスナップショットから使い捨て repo と bare の remote を作り、実際に push する
  - `tests/issues/test_issue_links_valid.sh`: ROOT_DIR を `pwd -P` に。件数の下限の判定が `pwd -P` と比べていたので、
    論理パス (macOS の `/var` → `/private/var`、symlink 越しの checkout) では下限が黙って外れていた
  - README.md / issues/README.md / setup.sh のコメントに pre-push を書いた

## 結果

- 変異検証 (`bin/mutate-verify`。全部、狙ったケースだけが red): 触ったかの判定を常に偽 / 常に真・検査対象を作業ツリーにする・
  落ちても exit 0・CHECKS からリンク検査を抜く・一覧から 1 本抜く・tree の差分を旧 log 方式に戻す・展開を `git archive` に戻す・
  D/R のトリガーを外す・links のときも全部回す・リンク検査の ROOT_DIR を `pwd` に戻す
  - 🚨 最後の 1 本は初回 green (rc=6)。テストが HEAD から作った fixture の中の検査を写していて、作業ツリーの変異が届いていなかった。作業ツリーの検査を写す形に直して red
  - 「落ちても exit 0」は mutate-verify の差分表示が途中で切れて一部のケースが見えなかったので、使い捨て worktree で手で当てて全出力を確かめた
- 敵対的レビュー (opus、観点を分けて 1 体ずつ 3 周):
  - 1 周目 (素通り): P1 merge commit の中だけの変更が `git log --name-only` に出ず素通り → 両端の tree の `git diff` に変更 /
    P2 export-ignore の attributes で `git archive` からファイルが消える → read-tree + checkout-index に変更 /
    P2 issue がリンクしている issues/ の外のファイルを動かしても回らない → 削除・移動でも回す / P2 巻き戻しの force push → tree の差分で解消
  - 2 周目 (回帰): P2 削除・移動で全部回すと master が赤い間は src/ の rename まで止まる → リンク系 2 本だけに / P3 展開先で件数の下限が効かない → 上の `pwd -P`
  - 3 周目 (2 周目の修正): P3 テスト 11 が実パスの TMPDIR では空振り → symlink の置き場を作り前提を assert /
    P3 `"` 等を含む名前は `"issues/..."` とクォートされて full から links に落ちる → 先頭の `"` を剥がす (実際の git の出力で確認)。
    修正は判定ロジックを増やさず直接の実測で確認したので、4 周目は回していない
  - 壊せなかった (実測): worktree からの push・本物の index を汚さない・2 つの worktree から同時に push・一時ファイルの残骸・SIGINT / SIGTERM・symlink の issue・5145 commit の範囲で 0.155 秒
- 検出しない形 (hook 冒頭に書いた。採らなかった指摘): 同じ push で検査スクリプト自体を弱める / 既に remote に在る壊れた commit を新しい ref で指すだけの push。
  どちらも脅威モデル (うっかり壊した issue の push) の外で、CI も同じ tree を見るので review の責務

## 残タスク

- なし。以下は決着済み (2026-09-28、ユーザーの判断):
  - 見送り: 対応方針 2 (pending / waiting / next への移動もリンクの張り直しと 1 組にする)。リンクが切れても pre-push が
    push を止めて直し方を出すので、実害は手で直す往復 1 回。**再開の trigger**: 移動のたびに pre-push で止まって手で直す往復が目立ってきたとき
    (`scripts/issue_done.sh` の移動先を引数で選べるようにするのが素直。今は移動先が done 固定で、pending / next の分岐は移動元の判定にしか使っていない)
  - 受容: 番号の重複そのものは防がない。ファイル名で番号を持ち複数のセッション・環境が並行して採番する以上、構造では無くせない
    (中央で配る仕組みは運用に対して重い)。方針は「早く見つけて安く直す」: この環境からの push は pre-push が止め、
    hook を通らない入口 (web・setup.sh を回していないマシン・`--no-verify`) からの分は CI が赤くなって見つかる
