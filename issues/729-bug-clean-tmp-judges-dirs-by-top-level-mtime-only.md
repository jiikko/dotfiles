# 729 (bug): `make clean-tmp` がディレクトリを直下の mtime だけで古いと判定し、中の新しいファイルごと消す

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


- [ ] 判定を「中で一番新しいファイル」に変える
- [ ] 残した理由を出力に出す
- [ ] find の失敗 (rc≠0) を判定不能として残す
- [ ] 登録済みの worktree を対象から外す
- [ ] テスト (古い dir + 新しい中身 / 空 dir / 0 件) と変異での red
