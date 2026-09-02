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

- [x] CLI と UI で svc の注記が一致する (fixture を両方に流すテスト)
- [x] 変異検証: 注記を落とすと parity テストが red になる

## 対応 (2026-09-02)

**注記の文言を `svc.Annotations` に一本化した** (単純に UI へ 2 行足すのではなく、二重管理そのものを消す形にした)。
各経路が自前で文言を持っていたことが原因で、片方にだけ注記を足すと食い違いが生まれる構造だった。

- `src/doctor/svc/report.go` に `Annotations(f Finding) []string` を新設。CLI の `Format`、UI の `svcSection`、
  `svcCopyText` (Y) の 3 経路がこれを使う。行頭の記号とインデントは呼び出し側が付ける
- **逆向きの欠落も同時に閉じた**: 「起動状態は不明 (system ドメインは一般ユーザーの `launchctl list` に出ない)」は
  **UI にだけあって CLI に無かった**。issue 本文は CLI → UI の欠落 2 件しか挙げていなかったが、同じ二重管理から
  出た同型なので `Annotations` に含めた (注記は全 4 種)
- `svcCopyText` の `台帳にあり=false` という暗号をやめ、`Homebrew formula: <name>` + 注記の形にした

### 検証

`TestDoctorSvcAnnotationsMatchCLI` (glogx) を追加。注記 4 種を網羅した fixture を CLI の `Format` と
UI の `lines()` と Y のコピー文の 3 経路に流し、すべてに同じ文言が出ることを固定する。
fixture が 4 種を網羅していること自体も assert している (欠けたテストが「通っても何も守らない」形になるため)。

変異検証 (使い捨て worktree で実施。3 本とも red を確認):

| 変異 | 結果 |
|---|---|
| UI の `svcSection` を修正前 (PenaltyBox と system ドメインだけ自前で出す) に戻す | red 「UI の svcSection に注記が無い」×2 |
| `svcCopyText` を修正前 (`台帳にあり=%v`) に戻す | red 「Y のコピー文に注記が無い」×4 + 暗号の検出 |
| CLI の `Format` から `Annotations` 呼びを消す (PenaltyBox だけ残す) | red 「CLI の Format に注記が無い」×3 |

### 残り (この issue の範囲外)

- 注記が**狭い幅で末尾切れする**問題は [issues/182](182-ux-doctor-display-width-issues.md) が扱う。
  parity テストは幅 240 で見ている (注記の有無を見るテストなので、幅の問題と混ぜない)
- 注記に**裏取りコマンド**を添える案は [issues/183](183-ux-doctor-copy-text-lacks-verification-commands.md)
- `Annotations` の brew 孤児の文言に `/opt/homebrew/var` が直書きされている。
  [issues/176](176-bug-doctor-homebrew-prefix-hardcoded.md) で prefix を動的化するとき、ここも 1 箇所直せば済む
  (一本化したので直す場所は 1 つ)
