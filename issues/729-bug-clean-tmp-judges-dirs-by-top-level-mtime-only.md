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
- `_claude/CLAUDE.md` の「消す前に issue や doc が指しているパスでないかを確かめる」(`grep -rn 'tmp/' issues/ _claude/`) も手作業のまま。
  open な issue (`done/` 以外) が参照しているエントリを一覧で印付けする案もあるが、参照の形 (`tmp/x/y` / `~/dotfiles/tmp/x`) の取り方が
  字句の判定になるので、入れるなら印付け (目安) に留め、除外の判定には使わない
- 破壊的操作の検査なので、直したら `adversarial-review-own-safeguards.md` の §1 の表 (0 件 / 空のディレクトリ / 権限が無い) と変異で red を確かめる。
  テストは今は無い (`tests/` に `clean-tmp` を参照するものは 0 件。2026-10-12 に grep)

## 関連ファイル

- `Makefile` の `clean-tmp` target (新設は commit「chore(tmp): make clean-tmp を新設し、issue から tmp を参照している箇所を直す」)
- `_claude/CLAUDE.md` の「一時ファイルの配置」/ `docs/README.md` の置き場の表 (`make clean-tmp` を案内している)

## 進捗

- [ ] 判定を「中で一番新しいファイル」に変える
- [ ] 残した理由を出力に出す
- [ ] テスト (古い dir + 新しい中身 / 空 dir / 0 件) と変異での red
