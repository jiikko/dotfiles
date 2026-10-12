# 733 (perf): treefiler の重さ (2026-10-12 の監査で実測したもの)

> 🚨 **担当中: dotfiles-4e**（2026-10-12〜）

起票日: 2026-10-12

## 概要

監査 (737) で実測した重さ。上ほど重い。数字は調査体が repo のコピーで `go test -bench` で測った値 (Apple M3 Max)。

## 詳細

1. **ライブ更新の 1 件が「変わった名前 × 子の数」の 2 乗** (P1・実測) — `filer/model.go` の `applyChange` の `for name in c.changed { for k in n.kids }`。
   開いた巨大なフォルダでほぼ全部が 1 周で変わる (`touch *`・unzip・`git checkout`) と、5,000 件で 63ms、**50,000 件で 11.6 秒** UI が固まる。
   changed を set にして kids を 1 回だけ回すと 50,000 件で 0.136 秒 (実測)
2. **検索の打鍵で `fuzzy` が名前ごとに `[]ch` を確保する** (P3・実測) — `filer/search.go` の `fuzzy`。5 万件の `BenchmarkSearchKeyHugeFolder` が
   12.05ms / 125,869 allocs / 32MB → スタックの配列 (`var stack [64]ch; n := stack[:0]`) で 8.31ms / 20,868 allocs / 2MB。issue 723 で「同じ判定の 2 つ目の実装になる」ので
   見送った ASCII の速い経路とは別の直し方 (判定は 1 つのまま)
3. 記録 (直さない): `direntries_darwin.go` の `parseBulkRecord` が 1 件ごとに `errors.New` (5 万件で allocs 115k → 65k・時間はほぼ同じ) /
   `node.reload` が開いた子へ再帰する (400 フォルダ × 15 件で 1 回 14.7ms。閉じて読み込み済みのフォルダは見張られないので再帰に意味がある)

## 関連

- 737 (この監査)・723 (検索)・693 (巨大なフォルダ)

## 進捗

- 2026-10-12: 起票 (監査 737)
- 2026-10-12 (dotfiles-4e): 1・2 を直した。commit「fix(treefiler): 監査の不具合 731 の 1〜6・性能 733・設計 734・テスト 735 を直す」
  - 1: `applyChange` は変わった名前を集合にして子を 1 回だけ回す。2: `fuzzy` はスタックの配列 (64 文字まで確保しない)
  - **実測** (Apple M3 Max。`perf_bench_test.go`。before は origin/master に同じベンチを置いて同じ実行で):
    `BenchmarkLiveUpdateAllChangedHugeFolder` n=50,000: **16.3 秒 → 0.125 秒**・n=5,000: 64ms → 10ms /
    `BenchmarkSearchKeyHugeFolder` n=50,000: 16.1ms / 160k allocs → 11.7ms / 26.5k allocs
  - 集合の引き方を逆にする変異で既存のライブ更新のテストが red。3 (記録) は直していない
