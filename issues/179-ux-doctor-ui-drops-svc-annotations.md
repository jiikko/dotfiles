# 179 ux: UI が svc の「com.apple. 偽装」「アンインストール済み formula」注記を落とす

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 6) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (4 章「登録条件」の A/B/C 判定)

## 対象

`src/glogx/doctor_view.go` の `svcSection` / `svcCopyText` (Y のコピー文)。CLI 側は `src/doctor/svc/report.go` の `Format`

## 何が起きるか

CLI は出しているのに UI が出していない注記が 2 つある (実証済み。体 6 の parity 比較)。

| 注記 (CLI `svc.Format` は出す) | UI (`svcSection`) | Y のコピー文 |
|---|---|---|
| 「`com.apple.` を名乗っていますが管理領域の外」(AppleLikeOut) | 無い | 無い |
| 「アンインストール済み formula の登録が残っている」(BrewOrphan) | 無い | `台帳にあり=false` という暗号だけ |

とくに前者は**`com.apple.` を名乗る launchd 登録がサードパーティ領域に居る**という判定で、
malware の常套手段でもある。CLI を叩かないと見えないのは説明可能性の穴。

## 再現手順

同じ Report を CLI と UI に流して並べる (体 6 は fixture でこれをやった)。
`svc.Format` の出力に上記の文言があり、`svcSection` の行と `svcCopyText` の文面に無いことを確認する。

## 対応案

- `svcSection` の行 (または Enter の詳細) に 2 つの注記を出す
- `svcCopyText` (Y) に人が読める形で入れる (`台帳にあり=false` ではなく「アンインストール済み formula の登録が残っている」)
- 併せて issue 183 (裏取りコマンド) の svc 行のコマンドを同じ文面に入れると、別セッションの LLM がそのまま確かめられる

## 受け入れ条件

- [ ] CLI と UI で svc の注記が一致する (fixture を両方に流すテスト)
- [ ] 変異検証: 注記を落とすと parity テストが red になる
