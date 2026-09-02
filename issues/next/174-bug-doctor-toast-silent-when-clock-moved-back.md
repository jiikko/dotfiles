# 174 bug: `LastNotifiedAt` が未来 (時計を戻した) だと、トーストが cooldown 後も永久に沈黙する

起票日: 2026-09-02
重要度: **P3**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 2) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「トースト」の cooldown)

## 対象

`src/glogx/doctor_view.go` (または `doctor_cache.go`) の `doctorStartupToast`

## 何が起きるか

cooldown の判定が `now.Sub(LastNotifiedAt) < cooldown` で書かれている。`LastNotifiedAt` が**未来**だと差は負になり、
この条件は常に真なので、トーストは出ない。時計を戻した (または NTP で大きく補正された / 別マシンのキャッシュを持ってきた) 後、
**cooldown が明けてもずっと沈黙する**。

同じコードベースの `loadDoctorSnapshot` と `doctorReuseFrom` は `age < 0` を弾いているので、
**この 1 箇所だけが非対称**になっている。

実測: 実証済み (demo test で `LastNotifiedAt` を 1 年先にすると戻り値が `""`)。

## 再現手順

`doctor-disk.json` の `last_notified_at` を未来の日時に書き換えて glogx を起動する。閾値を超える合計があってもトーストが出ない。

## 対応案

`age < 0` (負の経過) を「cooldown 明け」として扱う。`loadDoctorSnapshot` / `doctorReuseFrom` と同じ規律に揃える。

## 受け入れ条件

- [ ] `LastNotifiedAt` が未来のときトーストが出ることをテストで固定する
- [ ] 変異検証: `age < 0` の扱いを外すと沈黙が再現することを確認する

## 対応 (2026-09-03)

**修正した。** cooldown の判定を `cooledDown(c, now)` に切り出し、**`age >= 0` を条件に足した**
(`src/glogx/doctor_cache.go`)。`LastNotifiedAt` が未来なら「cooldown 明け」として扱う。
`loadDoctorSnapshot` と `doctorReuseFrom` は既に `age < 0` を弾いており、**ここだけが非対称**だった。

⚠️ **安全側の向きは場所によって違う**ので、コメントに明示した。`cooledDown` は「未来なら通知する
(安全側 = 沈黙しない)」に倒すが、同じセッションで一度入れた `saveCache` の `fresh` 判定は
「未来なら保護しない (危険側)」に倒れていた。判定の形が似ているぶん取り違えやすい
(敵対レビュー 2026-09-03 の指摘。`fresh` は別の理由で撤回したので現在は存在しない)。

### 変異検証

`age >= 0` を外す → `TestDoctorStartupToast` の未来時刻ループ 3 ケース (1 秒後 / cooldown ぶん後 /
1 年後) が**すべて** red。`LastNotifiedAt == now` ちょうどの境界ケースは `age >= 0` を経由しないので
不変 (正しい)。

### 敵対的レビュー

172 / 173 と同じ差分でまとめて 5 周通した (下記 172 の記録を参照)。174 に対する指摘は
「安全側の向きが `saveCache` 側と逆なのにコメントの書き方が似ていて取り違えやすい」の 1 件で、
コメントに明示して対応した。
