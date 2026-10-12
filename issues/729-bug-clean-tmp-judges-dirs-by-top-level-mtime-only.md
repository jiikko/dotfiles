# 729 (bug): `make clean-tmp` がディレクトリを直下の mtime だけで古いと判定し、中の新しいファイルごと消す

> 🚨 **担当中: dotfiles-4e**（2026-10-12〜）

起票日: 2026-10-12

## 概要

`make clean-tmp` (`Makefile` の `clean-tmp` target) は `find tmp -maxdepth 1 -mindepth 1 -mtime +$(DAYS)` で
`tmp/` 直下のエントリを選び、`rm -rf` する。ディレクトリの mtime は**直下のエントリを足す・消すときにしか**変わらず、
中のファイルを書き換えても (孫以下にファイルを足しても) 更新されない。そのため、古いディレクトリの中で今も使っているファイルを
丸ごと消しうる。

## 詳細 (再現)

実測 2026-10-12 (macOS / BSD find):

```sh
d=<空の一時ディレクトリ>
mkdir -p $d/old/sub && echo a > $d/old/sub/f
touch -t 202609010000 $d/old $d/old/sub $d/old/sub/f
echo b >> $d/old/sub/f            # 中のファイルを今日書き換える
touch -t 202609010000 $d/old/sub  # (sub の mtime は追記では変わらない。念のため固定)
find $d -maxdepth 1 -mindepth 1 -mtime +7
# → $d/old が出る (f の mtime は 2026-10-12 なのに対象になる)
```

- 期待: 中に `DAYS` 日以内に更新されたファイルがあるディレクトリは対象から外す (少なくとも一覧で区別する)
- 実際: 直下のディレクトリ自身の mtime だけで判定する
- 2026-10-11 に `DAYS=7` で 488 件を掃除したときは、手で `find <各候補> -mindepth 1 -mtime -7` を当てて 0 件を確かめてから消した。
  この確認は target に入っていない

## 対応方針 (案)

- 候補のディレクトリごとに「中で一番新しいファイルの mtime」で判定する (`find <dir> -newermt` / `-mtime -$(DAYS)` が 1 件でも当たれば残す)
- 残したものは DRY_RUN でも本実行でも「中に新しいファイルがあるので残した: <path>」と出す (黙って外さない)
- 🚨 **find が失敗したら「新しいファイルは無い」と読まず、残して報告する**。今の target も `list="$$(find ...)"` の rc を見ておらず、
  空の結果を「0 件」で exit 0 にする。中の判定を足すと、読めない子ディレクトリ (mode 000) で `find` が `Permission denied` / rc=1 を返し、
  出力が空なら「新しいものは無い」= 削除に倒れる (2026-10-12 に `/usr/bin/find` で rc=1 を確認)。rc≠0 は「判定不能 → 消さない」にする
- 判定に数えるのはファイルだけでなく子孫のディレクトリの mtime も含める (エントリの追加・削除はそこに出る)。`-L` は付けない
  (symlink の先で判定することになる。今は link 自身だけを `rm -rf` している)
- **`tmp/wt-*` の git worktree を対象から外す** (`git worktree list` に載っているパス)。worktree は `tmp/` の下に作る運用で、
  直下の mtime が古いまま中で作業中のことがあり、`rm -rf` すると作業を消したうえ `.git/worktrees` に登録が残る
- `_claude/CLAUDE.md` の「消す前に issue や doc が指しているパスでないかを確かめる」(`grep -rn 'tmp/' issues/ _claude/`) も手作業のまま。
  open な issue (`done/` 以外) が参照しているエントリを一覧で印付けする案もあるが、参照の形 (`tmp/x/y` / `~/dotfiles/tmp/x`) の取り方が
  字句の判定になるので、入れるなら印付け (目安) に留め、除外の判定には使わない
- 破壊的操作の検査なので、直したら `adversarial-review-own-safeguards.md` の §1 の表 (0 件 / 空のディレクトリ / 権限が無い) と変異で red を確かめる。
  テストは今は無い (`tests/` に `clean-tmp` を参照するものは 0 件。2026-10-12 に grep)

## 関連ファイル

- `Makefile` の `clean-tmp` target (新設は commit「chore(tmp): make clean-tmp を新設し、issue から tmp を参照している箇所を直す」)
- `_claude/CLAUDE.md` の「一時ファイルの配置」/ `docs/README.md` の置き場の表 (`make clean-tmp` を案内している)

## 進捗

- 2026-10-12 反証レビュー (sonnet・read-only 1 体): 核の主張 (mtime の挙動・再現・テスト 0 件) は反証されず。採用 3 件 = find の失敗が削除に倒れる (P1、実測で確認) / worktree を消す / 子孫ディレクトリの扱い。却下 1 件 = `find` が bfs に解決される環境の差 (repo は macOS のみで、make の PATH の find は `/usr/bin/find`)。記録のみ 1 件 = DRY_RUN のサイズが `du 2>/dev/null` で過小になりうる (消す対象は変わらない)


- [x] 判定を「中で一番新しいファイル」に変える
- [x] 残した理由を出力に出す
- [x] find の失敗 (rc≠0) を判定不能として残す
- [x] 登録済みの worktree を対象から外す (→ 登録で照合せず、中の git の印で残す形にした。下)
- [x] テスト (古い dir + 新しい中身 / 空 dir / 0 件) と変異での red

- 2026-10-12 (dotfiles-4e): 実装した。commit「fix(clean-tmp): 中に新しい更新・git の作業ツリーがあるフォルダを残し、判定できなければ消さない (issue 729)」
  - 本体を `scripts/clean_tmp.sh` へ移し (`make clean-tmp` が呼ぶ)、テスト `tests/scripts/test_clean_tmp.sh` (一時ディレクトリの git の repo の中だけで走る) を置いた
  - 判定: 自身の mtime が古い tmp 直下のエントリのうち、(1) 中に DAYS 日以内の更新がある・(2) 中に git の印 (`.git` を大文字小文字を問わず・HEAD (ファイルか symlink) と
    objects/・refs/ が並ぶ git dir の形) がある・(3) 中を読めない (find の失敗) ものは理由を出して残す。1 件ずつ調べてすぐ消す。tmp が repo の直下の実体でなければ止まる。
    `DRY_RUN` は 1 / 空・0 だけを受け、他の値 (true 等) は止まる
  - 変異 15 本 (中の更新を見ない・find の失敗を無視・worktree の判定を外す・前の版の照合に戻す・印の形を見ない・大文字小文字・here-string に戻す・symlink の HEAD・
    DRY_RUN を緩める・tmp の symlink を許す ほか) がすべて想定の検査で red
  - 敵対レビュー (opus・読み取りのみ) を 4 周:
    - 1 周目 P1: 登録済みの worktree をパスの文字列で照合していて、APFS の大文字小文字の違いで保護が外れた (実測) → dev:inode の照合へ。P2: DRY_RUN=true で消した → 知らない値は止まる
    - 2 周目 P1: worktree の経路を後から symlink に差し替えると実体を消した・P2: 動かしただけの worktree を消した (実測)。同じ不変条件を 2 周続けて破られたので (§7)
      パスの照合をやめ、中の `.git` の印で残す形に切り替えた (`git worktree list` への依存も消した)
    - 3 周目 P1: bare / --separate-git-dir の置き場 (.git を持たない) を消し、それを使う worktree の commit を失う (実測)・P2: .GIT → git dir の形と大文字小文字を問わない印を足した
    - 4 周目 P1: bash 3.2 の here-string が一時ファイルに書くので、ディスクが一杯だと印を見ないまま「消す」に倒れた (ulimit -f 0 で再現) → 変数を割って回す形にし、
      同じ条件の回帰テスト (10) を置いた (前の版で red)・P2: symlink の HEAD の bare を見逃す → 足した。4 周目の直しは判定を足さず実測と変異で確かめたので周回を打ち切った
  - 壊せなかったもの (各周): find の -mtime の境界 (+N と ! +N は同じ丸めの補集合)・未来の mtime・先頭が - や改行の名前・tmp の symlink / repo の外 / 読めない tmp・
    子孫の symlink を辿らない・reftable・本物の tmp で残す側に化けない (`~/dotfiles/tmp` 1000 エントリで印 0 件、他の repo の tmp 8 つも 0 件)
  - 検出しない (記録): 印がまったく無い作業ツリー (外の GIT_DIR・.git が消えた worktree)・名前に改行を含む git dir・tmp の下のマウントポイントへの rm -rf
    (旧実装から同じ。未実測)・mtime で見るので、書かずに読み続けている参照データは消える (設計の範囲)。packed-refs だけで refs/ が無い dir は git も repo と認めない
