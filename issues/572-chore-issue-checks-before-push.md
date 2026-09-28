# 572 (chore): issue の整合検査 (番号の一意性・相対リンク) を push の前に止める

> 🚨 **担当中: dotfiles-f9**（2026-09-28〜）

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

- [ ] `issues/` を触る push で、番号の重複 / 相対リンク切れがあると push が止まる (fixture で番号を重複させて確かめる)
- [ ] `issues/` を触らない push では検査を回さない (待ち時間を増やさない)
- [ ] 検査が走ったことが出力に出る (skip も skip と出る)
- [ ] pre-push を足したことを `README.md` の githooks の節に書く

## 関連ファイル

- `githooks/pre-commit` / `setup.sh` (`core.hooksPath`)
- `tests/issues/test_issue_numbers_unique.sh` / `tests/issues/test_issue_links_valid.sh`
- `scripts/issue_done.sh`
- `issues/README.md` の「採番」節

## 進捗

- 2026-09-28: 起票。反証レビュー (sonnet 1 体、読み取り専用): 事実の主張はすべて裏が取れた (commit・author・時刻・引用・「pre-push は無い」)。P3 1 件 (issue_done.sh の分岐は移動元の判定であり、移動先ではない。書き方を明確にした) を反映
