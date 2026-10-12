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
- 一致の確認は手元の計測コードで、symlink (絶対・相対・宙ぶらりん)・fifo・chmod 000 のフォルダ・NFC と NFD の名前を混ぜても全件一致した
  (反証レビューの試行)。**repo に一致の検査はまだ無い**

## 効果の大きさ (見積もり)

- **体感に効くのは `load` / `reload`**: 巨大なフォルダを開く・読み直す待ちが 5 万件で約 70ms 縮む
- `readSig` (ライブ更新の監視) は周期 1 秒 (`watch.go` `watchEvery`) で、5 万件のフォルダは間引き (issue 693) で 5 周に 1 回しか読まない。
  CPU は 154ms / 5 秒 ≒ 3% が 47ms / 5 秒 ≒ 1% になる見積もりで、差は約 2% (issue 693 の「16%」は間引きの前の値)
- **やらない条件**: cgo を足す費用 (下) に対して、5 万件級のフォルダを日常的に開かないなら得が小さい。
  再開の trigger は「巨大なフォルダを開く待ちが遅いと感じた」とき

## 対応方針 (案。未設計)

- 効く順: `load` / `reload` (巨大なフォルダを開く待ち) > `readSig` (間引き後の周期的な読み) > `walkDir`
- 呼び出し方を先に決める:
  - `golang.org/x/sys/unix` (v0.47.0) にラッパーは無く、`SYS_GETATTRLISTBULK` (461) の番号があるだけ
  - ① cgo で libc の `getattrlistbulk` を呼ぶ。glogx は既に cgo を使っている (`src/glogx/ime_tis_darwin.go`。`//go:build darwin && cgo`
    で `ime_tis_stub.go` と対)。treefiler の module は使っていない。🚨 採るなら、CGO_ENABLED=0 のビルドで今の形 (ReadDir + Info) に戻る
    stub と、CI で bulk の経路が実際にコンパイルされて走ることの確認が要る (`.github/workflows/_go-project.yml` に「CGO_ENABLED=0 だと
    黙って除外される」の実測の注記がある)
  - ② `unix.Syscall` で番号を直接呼ぶ。darwin の Go は libc 経由が前提なので、動くかと保守性は未確認
- 返ってくる buffer (`attribute_set_t`・`attrreference_t` の位置と可変長の名前) を読む処理は自前になる
- 🚨 `load` は symlink を辿らない (lstat。`tree.go` の node の doc)。bulk は既定で辿らないはずだが、symlink の項目の値を突き合わせた検査は
  まだ無い (計測のフォルダに symlink が無かった)。入れる前に、symlink・読めない項目・読む途中で消えた項目を混ぜた一致の検査を置く
- 失敗したとき (未対応のファイルシステム等) に今の形 (ReadDir + Info) へ戻る経路を持つかを決める
- 計測コードにあった穴 (production に持ち込むときに塞ぐ): 戻り値が負 (SMB などの ENOTSUP) を空の結果と区別していない /
  `returned.commonattr` で属性が実際に返ったかを見ず、`FSOPT_PACK_INVAL_ATTRS` も指定していない (返らない属性があると位置がずれて誤読する) /
  一致の検査が今の方式の側の名前しか回さない (bulk にだけ在る項目は件数でしか拾えない)
- `readSig` が速くなれば `bigDirEvery` の間引き (issue 693) を緩める余地が出る。緩めるかは別の判断

## 受け入れ条件 (案)

- [x] 5 万件のフォルダで `readSig` / `load` の時間を前後で測り、issue に書く (同じ条件で)
- [x] symlink・読めない項目・途中で消えた項目を含むフォルダで、今の形と結果が一致する検査がある (両方の側の名前の集合を突き合わせる)
- [x] 失敗 (戻り値が負) と部分的な結果を区別し、返った属性を `returned` で確かめている

## 別件 (この issue の範囲の外。未確認)

- doctor `src/doctor/disk/size.go` も `filepath.WalkDir` の中で項目ごとに `d.Info()` を打ち、`Stat_t` から Dev / Ino / Blocks を取っている。
  bulk に寄せられるか (Blocks*512 と `ATTR_FILE_ALLOCSIZE` が一致するか・Dev / Ino を取れるか) は未確認
- `walk.go` `walkDir` の冒頭の `os.Lstat(dir)` は、親が ReadDir のループで既に `e.Info()` を取った項目を読み直している。親から info を
  渡せば bulk と関係なく 1 回減らせる (小さな改善)

## 関連

- issue 693 (巨大なフォルダの重さ。走査・ライブ更新の間引き) / issue 712 (配置・描画の仮想化。読む側ではなく描く側)

## 進捗

- 2026-10-12: 起票 (dotfiles-5f の調査から)
- 2026-10-12: 反証レビュー (dotfiles-5f、sonnet) を当てた。効く順を逆に (体感は load)・効果の大きさとやらない条件・cgo の stub と CI・
  計測コードの穴・doctor の size.go (別件) を足した。反証できなかった主張: 定数 (5000 / 5)・3 か所の N+1・explode.go が Info を打たない・
  cgo の有無・load が lstat・x/sys にラッパーが無い
- 2026-10-12: issue 712 (描く側の仮想化) は done。この issue (読む側の N+1) は継続
- 2026-10-12 (dotfiles-4e): 実装した。commit「perf(treefiler): フォルダを getattrlistbulk でまとめて読む (issue 714)」
  - **呼び出し方は ② (cgo なし)**: `unix.Syscall6(unix.SYS_GETATTRLISTBULK, …)`。darwin の Syscall6 は libc の syscall(2) を通る。cgo を足さないので
    CGO_ENABLED=0 の stub と CI の確認の問題が出ない (他の OS は `direntries_other.go` が今の形へ回す)
  - `filer/direntries.go` の `readEntries` に寄せ、`load`・`readSig`・`walkDir` が使う。まとめて読めない (errno・壊れた記録) ときはフォルダごと今の形
    (`readEntriesSlow` = ReadDir + lstat) へ戻る。属性が返らない項目・ATTR_CMN_ERROR の立った項目・**フォルダの項目**はその項目だけ lstat で読み直す。
    walkDir は名前順に並べ直す (budget で打ち切る位置を os.ReadDir の頃と変えない)
  - 記録の並び (実測): 返った属性 (5 語) → ERROR → 名前の参照 → 種類 → mtime → 大きさ 1 つ (フォルダは ATTR_DIR_DATALENGTH・それ以外は
    ATTR_FILE_DATALENGTH の片方だけ。両方は詰まらない)。issue の「計測コードの穴」(負の戻り値・returned を見ない・PACK_INVAL 無し・片側の名前しか回さない) は全部塞いだ
  - **実測** (Apple M3 Max・go1.26.0・APFS・キャッシュに載った状態。`perf_bench_test.go` の `BenchmarkReadHugeFolder` (ファイル 9 割・フォルダ 1 割)。
    before は origin/master に同じベンチを置いて同じ実行で測った。`-benchtime 21x`):

    | | before | after |
    |---|---|---|
    | `readSig` n=50,000 | 154ms | 59ms |
    | `load` n=50,000 | 174ms | 129ms (残りは node を作る・名前の自然順の sort。この issue の外) |
    | `readSig` n=5,000 | 41ms (冷えていた可能性) | 3.8ms |

  - 一致の検査: `TestBulkMatchesReadDirLstat` (symlink 3 種・fifo・読めないフォルダ・NFC / NFD・255 バイトの名前・dotfile。**macOS で bulk が失敗したら落ちる** =
    今の形へ戻る経路だけが試される素通りを止める)・`TestBulkReadsAcrossBuffers` (64KB を超える 1500 件)・`TestBulkMatchesAtFirmlinksAndMountPoints` (`/`・`/System/Volumes`・`/Volumes`)・
    `TestReadEntriesFallsBack`・`TestParseBulkRecordRejectsMalformed`・`TestParseBulkRecordFallsBackPerEntry`・`TestWalkDirCountsInNameOrderUnderBudget`
  - 変異 9 本 (mtime の ns・フォルダの判定・項目ごとの lstat・名前の境界・今の形へ戻る経路・walk の並べ直し・大きさ・フォルダを bulk の値で読む) を当て、全部想定の検査で red
  - 敵対レビュー (opus・読み取りのみ): **P2 を実測で再現** — マウントポイント・ファームリンク (`/` の Users・Applications 等 9 件・/Volumes・自分で mount した HFS+ / ExFAT / FAT)
    では bulk が覆われた側の mtime・大きさを返した → フォルダの項目は lstat で読み直す形で直し、`/` の実物で固定 (bulk の値を使う変異で red)。直しは今の形 (正解役) へ回すだけで
    新しい判定を足さず、実物と変異で確かめたので周回はここで打ち切った。
    壊せなかった: APFS / HFS+ / FAT32 で一致 (圧縮・hardlink・symlink・NFC/NFD・400 件 × 150 文字・/dev・iCloud)・r-- / --x のフォルダは前と同じ結果・境界の読み・fd・Pool・unsafe の渡し方・無限ループ
  - 記録 (直していない): FSKit の exFAT で AppleDouble (`._*`) の mtime が bulk と lstat で 10ms ずれる (818 件中 10 件。表示はほぼ無害。bulk と今の形が交互に使われると
    readSig が偽の変化を出しうるが、途中で bulk が失敗する条件は見つかっていない)。SMB / NFS は未実測 (属性が返らない項目は 1 件ずつ lstat なので往復は前と同じはず)
