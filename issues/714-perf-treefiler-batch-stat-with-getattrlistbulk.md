# 714 (perf): treefiler はフォルダを読むたびに項目ごとに lstat を打つ (getattrlistbulk でまとめる)

起票日: 2026-10-12

## 概要

treefiler はフォルダの中身を `os.ReadDir` で読む。名前の一覧は getdirentries でまとめて取れているが、その後に**項目ごとに `e.Info()`
(= lstat) を 1 回ずつ**打っている (N+1)。5 万件のフォルダだと lstat を 5 万回打つことになる。
macOS の `getattrlistbulk(2)` を使えば、名前と属性 (mtime・大きさ・種類) を 1 回の syscall でまとめて取れる。

## 該当箇所 (master の版。2026-10-12 に確認)

| 箇所 | 打つ数 | 走る頻度 |
|---|---|---|
| `src/treefiler/filer/watch.go` `readSig` | ReadDir 1 + lstat × 項目数 | ライブ更新の監視 1 周ごと。`bigDirEntries` (5000) を超えるフォルダは、mtime が変わらない限り `bigDirEvery` (5) 周に 1 回 (issue 693) |
| `src/treefiler/filer/tree.go` `(*node).load` (`reload` は開いている子へ再帰する) | ReadDir 1 + lstat × 項目数 | フォルダを開く・読み直すたび |
| `src/treefiler/filer/walk.go` の配下の走査 (`walkDir`) | フォルダごとに lstat 1 + ReadDir 1 + lstat × 項目数 | 配下の大きさ・新しさを数えるとき (budget で上限あり) |

N+1 ではないもの:
- `explode.go`: ReadDir の後に `e.IsDir()` (getdirentries の d_type) しか使わず、`Info()` を打たない
- glogx の issues viewer の指紋 (`issues_watch.go` `issuesFingerprint`): issue ファイルごとに os.Stat を打つが、717 件で 0.69ms。
  フォルダ 13 個を getattrlistbulk で読む形は 0.83ms で**遅かった** (issue 以外の項目も読む・1 回ごとに 256KB の buffer を確保する)。直す理由が無い
- doctor `disk/guard.go` `processRunning` の pgrep を名前ごとに起こす形: production の名前は計 5 つで、スキャン 1 回に約 0.1 秒。
  1 回にまとめると「どの名前が居たか」を理由の文に出せなくなる

## 実測 (dotfiles-5f が 2026-10-12 に測った。計測コードは別 module・cgo で、repo には入れていない)

条件: Apple M3 Max・macOS (Darwin 27.0.0)・APFS の内蔵 SSD・go1.26.0 darwin/arm64。同じプロセスで 1 回温めてから 21 回測った
中央値 (最小〜最大)。**キャッシュに載った状態の値** (冷えたキャッシュは未測定)。両方式の結果 (名前・mtime (ns)・種類・大きさ) は全件一致した。

| 対象 | ReadDir だけ (名前) | ReadDir + 項目ごとの Info (今) | getattrlistbulk |
|---|---|---|---|
| 5 万件 (ファイル 45,000 + フォルダ 5,000) | 27.2ms | **115.5ms** (104〜141) | **47.1ms** (43〜55) |
| 200 件 | 0.12ms | 0.31ms | 0.11ms |

- 5 万件で約 2.5 倍速い。watch.go のコメントにある「5 万件で readSig 1 回 154ms」(issue 693 の実測) と同じ桁
- 縮むのは lstat を項目数ぶん打つ分 (約 70ms / 5 万件 ≒ 1.4µs/件)

## 対応方針 (案。未設計)

- 効く順: `readSig` (周期的に走る) > `load` / `reload` (巨大なフォルダを開く待ち) > `walkDir`
- 呼び出し方を先に決める:
  - `golang.org/x/sys/unix` (v0.47.0) にラッパーは無く、`SYS_GETATTRLISTBULK` (461) の番号があるだけ
  - ① cgo で libc の `getattrlistbulk` を呼ぶ。glogx は既に cgo を使っている (`src/glogx/ime_tis_darwin.go`)。treefiler の module は使っていない
  - ② `unix.Syscall` で番号を直接呼ぶ。darwin の Go は libc 経由が前提なので、動くかと保守性は未確認
- 返ってくる buffer (`attribute_set_t`・`attrreference_t` の位置と可変長の名前) を読む処理は自前になる
- 🚨 `load` は symlink を辿らない (lstat。`tree.go` の node の doc)。bulk は既定で辿らないはずだが、symlink の項目の値を突き合わせた検査は
  まだ無い (計測のフォルダに symlink が無かった)。入れる前に、symlink・読めない項目・読む途中で消えた項目を混ぜた一致の検査を置く
- 失敗したとき (未対応のファイルシステム等) に今の形 (ReadDir + Info) へ戻る経路を持つかを決める
- `readSig` が速くなれば `bigDirEvery` の間引き (issue 693) を緩める余地が出る。緩めるかは別の判断

## 受け入れ条件 (案)

- [ ] 5 万件のフォルダで `readSig` / `load` の時間を前後で測り、issue に書く (同じ条件で)
- [ ] symlink・読めない項目・途中で消えた項目を含むフォルダで、今の形と結果が一致する検査がある

## 関連

- issue 693 (巨大なフォルダの重さ。走査・ライブ更新の間引き) / issue 712 (配置・描画の仮想化。読む側ではなく描く側)

## 進捗

- 2026-10-12: 起票 (dotfiles-5f の調査から)
