# doctor の snapshot 復元経路が `Entry` をカタログへ再束縛せず、保存された表示文言をそのまま使う

起票日: 2026-09-04
種別: bug (信頼境界 / 表示のドリフト)
優先度: **P2** (削除経路は影響を受けない。実害は表示と `y` / `Y` のコピー文)
出典: audit (leaky-abstraction) 2026-09-03 の索引 [7]。
**出典は「live 経路が termsafe を通らない」(issue 228) と同じクラスタで報告されたが、反証レビューが
「修正箇所も回帰テストも別物なので 1 つの修正で閉じない」と指摘したため分離した。**

## 症状

`doctor_cache.go: doctorSnapshotInCatalog` は **ID がカタログに在るかだけを見て、`Entry` を差し替えない**:

```go
func doctorSnapshotInCatalog(rs []disk.Result, has func(string) bool) []disk.Result {
	out := make([]disk.Result, 0, len(rs))
	for _, r := range rs {
		if has(r.Entry.ID) {
			out = append(out, r)   // ← r.Entry は snapshot に保存されていたもののまま
		}
	}
	return out
}
```

**姉妹経路は差し替えている** (`doctor_cache.go:685`):

```go
r.Entry = e // 表示文言はカタログの今の定義に合わせる (計測値だけを再利用)
```

同じファイルの中で、計測値の再利用 (`Reused`) は「表示文言は今のカタログに合わせる」と決めているのに、
画面ごとの復元 (`FromSnapshot`) はそれをしていない。

## 確認したこと (2026-09-04 実測)

1. `doctorSnapshotInCatalog` に `r.Entry` への代入は無い (上に全文引用)
2. `doctor_cache.go:685` の姉妹経路は `r.Entry = e` で再束縛し、**理由をコメントに書いている**
3. **`sanitizeSnapshotResults` は `Entry` を一切触らない** (関数本体に `Entry` の出現 0 件)。
   `Item.Path` は `safeDisplayPath` で `unicode.IsPrint` 検査を受けるが、`Entry.Label` / `Entry.Risk` /
   `Entry.Detail` は**検査も置換も受けない**
4. `disk.Entry` に json タグは無く、`~/.cache/glog/doctor-snapshot.json` には
   `ID / Label / Tier / Risk / Recover / Detail / DeleteVia / Paths / Guard / Processes / Inspect` が**丸ごと保存**される
5. 表示側は `doctorRiskMark` が `string(r.Entry.Risk)` を返し、そのまま列に載る

### 削除経路は影響を受けない (裏取り済み)

- `src/doctor/disk/delete.go:354,357` — `t.Reused` / `t.FromSnapshot` を**拒否**する
- `src/doctor/disk/delete.go:364` — `e, ok := lookupEntry(opt.Catalog, t.Entry.ID)` で
  **コンパイル済みカタログから ID で再解決**し、再走査もその `e` で行う

したがって細工した `Entry.DeleteVia` / `Paths` が削除の作法を乗っ取ることは**無い**。
実害は**表示と `y` / `Y` のコピー文**に限定される。

## 🚨 その関数自身の doc が、もう成立していない前提を書いている

`doctor_cache.go: sanitizeSnapshotResults` の doc (issue 178 由来):

> 🚨 **信頼境界**: `doctor-snapshot.json` は一般ユーザー権限で書き換えられる。**今は削除機能が無いので
> 実害は表示だけだが**、④ (削除) はこの画面の行を対象にする設計なので、境界をここで確定しておく。
> 細工した JSON の任意パスが行・y のコピー・合計・次の snapshot への書き戻しに載ってはいけない。

- 「**今は削除機能が無いので**」は **2026-09-03 に ④ が着地した時点で古くなった**。
  今の正しい説明は「削除経路は `FromSnapshot` を拒否し、カタログから ID で再解決するので影響を受けない」
- そして doc が守ると宣言している「細工した JSON の任意パスが**行・y のコピー**に載ってはいけない」は、
  `Entry.Label` / `Entry.Detail` / `Entry.Risk` について**現に守られていない** (上記 3)

## 発火条件

| # | 条件 | 結果 | 気づけるか |
|---|---|---|---|
| 1 | カタログの `Label` / `Risk` / `Detail` を更新し、古い snapshot から復元する | 画面に**古い文言**が出る (計測値だけでなく説明文も過去のもの) | silent。build もテストも通る |
| 2 | `~/.cache/glog/doctor-snapshot.json` の `entry` を書き換える | 任意の文言が行と `y`/`Y` のコピー文に載る。**制御文字も落ちない** (`Entry` は sanitize 対象外) | silent |

条件 2 の脅威モデルは**この repo が既に採用しているもの** (`issues/done/178` / `done/193` が
「snapshot は一般ユーザー権限で書き換えられる」を前提に境界を引いている)。新しい仮定を持ち込んでいない。

## 対応方針

### 最小

`doctorSnapshotInCatalog` を、**姉妹経路 (`doctor_cache.go:685`) と同じ形**にする —
カタログの `Entry` を引いて `r.Entry = e` で差し替える (`has(id)` を `lookup(id)` に変えるだけで足りる)。
これで条件 1 (ドリフト) と条件 2 (細工した文言) の**両方**が同時に閉じる。
`Entry` が常にコンパイル済みカタログ由来になるため、`Entry` を sanitize する必要そのものが消える。

### 変異検証

「`entry.Label` を細工した snapshot を書いて復元し、画面にカタログの `Label` が出る」テストを書き、
**再束縛を外す変異で red** になることを確認する。
条件 1 側は「カタログと違う `Label` を保存した snapshot」で同じテストが兼ねられる。

### doc の更新 (同じ変更で)

`sanitizeSnapshotResults` の「今は削除機能が無いので実害は表示だけ」を、
**現状 (削除は `FromSnapshot` を拒否 + カタログから ID で再解決するので影響を受けない)** に直す。
古い前提を残すと、次に読む人が「削除はこの境界に依存している」と誤読する。

## 関連

- issue 228 (doctor の **live** 経路が termsafe を通らない。同じクラスタで報告されたが別物)
- `issues/done/178` / `issues/done/193` — snapshot の信頼境界を引いた issue。本件はその**取りこぼし**
- `src/doctor/disk/delete.go` — `FromSnapshot` を拒否し ID で再解決する側 (本件の影響を受けない理由)
