# 909 (task): obaket の make test と CI に dupscan gate を組み込む

起票日: 2026-10-25

> **obaket の issue 1301 へ移した** (2026-10-25。ユーザー指示: dupscan のソースは dotfiles に残し、obaket 側のタスクは obaket の issue へ移す)。
> 手順・受け入れ条件・実測はそちらが正本。PR 24 の前の木で確かめた dotfiles の道具の受け入れ (905・904・897・898・894・896) は、
> 1301 の段階 2 で測り、結果を dotfiles の各 issue に書き戻す。

## 決着

- 2026-10-25 本文 (前提の実測・PR 24 の前の木での受け入れ・組み込み・CI での確かめ・skill への入口) を obaket の issue 1301 へ移した。
  移したときに足したユーザーの決定: CI は GHA の `ubuntu-slim` で回す (Swift を実行しないため)・CI に入れるのは最後 (出力を見てから)・
  除外の仕組みが要る段で止める (別のマシンでノイズを除外する仕組みを作っている)
- 起票時の「このマシンには obaket の checkout が無い」は kojiM3MBP のことで、M4Pro には obaket と dotfiles の両方がある
