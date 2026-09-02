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
