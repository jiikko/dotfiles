# 035 refactor: issues viewer の見張りを無変化が続いたら間引く (trigger 待ち)

issues viewer の live reload (issue 034) は、viewer を開いている間ずっと 1 秒周期の tick を回す
(`src/glogx/issues_watch.go`)。glogx は「**動くものがある間だけ tick を回す**」を設計原則にしており
(フレーム tick は `spinnerActive()` が false になれば再アームせず止まる。`tui.go` の `tickMsg`)、
開いたまま放置された viewer は「動くものが無いのに起きている」状態に当たる。無変化が続いたら周期を落とす (backoff) 余地がある。

**ただし実測では今すぐ直す理由が無い。着手は下記の trigger 待ちとする。**

## 実測 (2026-08-01、この repo)

| 項目 | 値 |
|---|---|
| 監視対象 | 1 ディレクトリ + 34 ファイル |
| `issuesFingerprint` 1 周期 | **46µs** |
| tick 1 回で走る再描画 (`view_steady`) | **~0.2ms** (CI Bench の直近 7 本の median) |
| 合計 | **~0.25ms/s = 1 コアの 0.025%** |

CPU の観点では無視できる。数字が効いてくるのは次の 2 つで、どちらもまだ観測していない:

- **ファイル数**: 指紋のコストは対象ファイル数に比例する。実測 34 ファイルで 46µs なので、
  400 ファイルの repo (docs/issues-viewer-spec.md が挙げる dropbox / DualNote 規模) では
  ~550µs/s。それでも 0.06% だが、桁が 1 つ上がる
- **wakeup**: ノート PC で viewer を開いたまま放置すると、1s タイマーが深い idle 状態を妨げる。
  CPU 時間ではなく電力の話なので上の表には出ない

## 案: 無変化が続いたら周期を落とす

`issuesWatch` に「無変化が続いた回数」を持たせ、`handleWatch` が次の tick の遅延を返す:

```
1s (既定) → 無変化 10 回 (10s) → 2s → 無変化 10 回 (20s) → 5s (上限)
変化を検出したら即 1s へ戻す (編集セッション中は反応を落とさない)
```

副作用: 放置後の最初の反映が最大 5 秒遅れる。別プロセスの編集は自分の打鍵と同期しないので
「即時」の体感は保たれるはず (issue 034 の判断と同じ理屈) だが、**放置した viewer をチラ見した
瞬間に古い内容が出る**ケースは増える。ここが実装の是非を分ける。

代替案 (採らない):
- **キー入力があった間だけ 1s、それ以外は止める**: viewer を眺めているだけ (打鍵しない) の
  状態がまさに live reload の主用途なので、目的と反する
- **fsnotify へ移行**: issue 034 で比較済み (watcher の生成/破棄・イベントのバーストの debounce・
  ディレクトリ追加時の再登録・NFS で無音、を 1s の遅延差のために抱えない)

## 着手の trigger (どれかが起きたら)

- [ ] viewer を開いたままの放置でバッテリー消費・ファン回転が体感できた
- [ ] issue が 300 件を超える repo で viewer を常用するようになった
- [ ] 見張りの対象が増えた (本文以外のファイルも見るようになった等) でコストが桁で変わった

trigger 無しでの先回り実装はしない (`_claude/rules/verify-design-intent-before-refactor.md` の
「speculative refactor より実変更 trigger 待ち」)。状態を 1 つ増やすぶん、`handleWatch` の
状態機械 (基準取り / 安定待ち / 保留) が 4 状態目を持つことになるのも見合いにする。

## 検証 (着手するとき)

- 無変化が続くと遅延が 1s → 2s → 5s と伸び、変化を検出したら 1s へ戻る (`handleWatch` の
  単体テストで固定。tea.Tick を待たずに駆動できる既存の形をそのまま使う)
- 放置 → 編集 → 最初の反映が上限周期以内に来る
- viewer を閉じたら止まる (既存テスト `TestIssuesWatchStopsWhenClosed` が引き続き green)
- `make -C src/glogx test` / Bench (描画パスは触らないが tick 周りなので確認)

codex レビュー未通過 (codex 不使用の運用指示による)。
