# 277 bug: glogx issues view の `rebindOpen` が basename 一致で別 issue に繋ぎ直しうる

起票日: 2026-09-06
種別: bug (未再現。obaket 719 の D3 P1-7)

## 症状 (想定)

`src/glogx/issues_view.go` の `rebindOpen` は、開いていた issue の path が scan 後に無いとき、同じファイル名 (basename) の issue が
**ちょうど 1 件**あればそれを本人とみなして繋ぎ直す (複数なら現状維持、0 件なら畳む)。`issues/epic/<name>/` で group が増えると、
同じ番号 + スラッグのファイルが別 group / 別 repo 階層 (`macOS/issues/`) に現れる余地が増え、「1 件だけ一致 = 本人」の前提が崩れる。
起きるのは「開いていた本文が、別の issue の本文に静かに差し替わる」形。

## 確認すること

- 同名 basename が別 group に 1 件ある状態で元ファイルを消し、rebindOpen が繋ぎ直す先を観測する (テストで作れる)
- 本人性を basename でなく内容 (番号 + 冒頭見出し) や inode / 移動履歴で決められないか

## 関連

- obaket `issues/719-retro-epic-dirs-tooling-2026-09-05.md` (起源)。同 retro の「敵対レビューで却下・記録した指摘」に P1-7 として記録
