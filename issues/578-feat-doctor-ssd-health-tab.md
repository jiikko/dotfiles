# 578 feat: glogx doctor に SSD 診断タブを追加する

> 🚨 **担当中: dotfiles-43**（2026-09-29〜）

起票日: 2026-09-29
重要度: **P2**

## 概要

glogx の `D` doctor に、既存の `Disk / Service / Homebrew / Docker` と並ぶ **SSD 診断タブ**を追加する。

既存の Disk タブは「容量を食っている既知の掃除候補」を見つけて削除するための機能であり、
SSD 自体の故障兆候・摩耗・I/O エラー・APFS の論理不整合は見ない。
今回追加する SSD タブは **ストレージの健康状態を読むだけ**に責務を限定し、削除・修復・設定変更はしない。

2026-09-28 に、メモリリークで macOS がクラッシュした実機があり、
「メモリリークそのもの」と「クラッシュ後に SSD / APFS 側へ異常が残っていないか」を切り分ける入口が欲しくなった。

## 背景 / 既存機能との境界

現状の doctor は `src/glogx/doctor_view.go` の `doctorTab` が次の 4 タブを持つ。

- Disk — 容量・掃除候補
- Service — launchd の壊れた登録
- Homebrew — `brew doctor`
- Docker — 未使用資源

SSD 診断は Disk タブへ混ぜない。
同じ「disk」という語でも、見ているものが違うため。

| タブ | 問い |
|---|---|
| Disk | 何が容量を使っていて、何を安全に掃除できるか |
| **SSD** | SSD 自体に故障兆候・摩耗・整合性エラーが無いか |
| SSD 内の APFS | SSD が正常でも、ファイルシステムの論理不整合が無いか |

情報源として `docs/macos-health-check.md` に 2026-09-27〜28 の実機調査が既にある。
同文書では SSD を将来の `H` ヘルス画面へ置く案になっているが、**SSD 部分は doctor の専用タブへ寄せる**。
`H` 全体を廃止するかはこの issue の範囲外。

## ゴール

1. SSD の「物理的な健康状態」と「APFS の論理整合性」を別々に確認できる
2. 通常の doctor 表示では軽い読み取りだけ行い、重い検査は明示操作でだけ走らせる
3. SMART が正常でも APFS に不整合があるケースを「正常」に丸めない
4. `smartctl` が無い・権限不足・timeout・未知出力を **ok にしない**
5. シリアル番号など不要な識別情報を UI / snapshot / copy text に残さない
6. doctor から `repairVolume`・`fsck`・sudo 等の修復操作は実行しない

## 表示案

タブ名は `SSD`。

例:

```text
[ Disk ] [ Service ] [ Homebrew ] [ Docker ] [ SSD ]

SSD
  SMART             ✓ Verified
  Critical Warning  ✓ 0x00
  Wear              7% used
  Available Spare   100%
  Integrity Errors  ✓ 0
  Unsafe Shutdowns  11
  Data Written      119 TB
  Power On Hours    3,982 h

APFS
  Data volume       未検査
  [Enter] 詳細   [r] 再診断   [v] APFS検証
```

キーは既存 doctor のキー設計と衝突確認してから決める。
上記 `r` / `v` は説明用で、実装時の固定仕様ではない。

## 取得項目

### 1. 軽量 — タブを開いたときに取得してよい

`diskutil info disk0`

最低限:

- `SMART Status`
- デバイス名 / Protocol など、診断対象がどのストレージかを識別するための非秘匿情報

判定:

- `SMART Status: Verified` = ok
- それ以外 = 異常
- 欄が無い / パース不能 / command failure = 判定不能

ただし **SMART Status が Verified でも「SSD 全体が完全に正常」とは表示しない**。
これは合否の粗い信号であり、摩耗率や media integrity error は `smartctl` 側で見る。

### 2. 明示実行 — SMART / NVMe の詳細

`smartctl -a disk0`（Homebrew `smartmontools`）

読む欄:

- `Critical Warning`
- `Percentage Used`
- `Available Spare`
- `Available Spare Threshold`
- `Media and Data Integrity Errors`
- `Data Units Written`
- `Power On Hours`
- `Unsafe Shutdowns`

判定の初期案:

- `Critical Warning != 0x00` → 異常
- `Media and Data Integrity Errors >= 1` → 異常
- `Available Spare <= Available Spare Threshold` → 異常
- `Percentage Used >= 80%` → 注意
- `Unsafe Shutdowns` / `Data Units Written` / `Power On Hours` → 数字だけ表示し、単独では異常判定しない

`Percentage Used >= 80%` は現時点では仮閾値。
実装時に根拠を再確認し、根拠が弱ければ「表示のみ」に落とす。

#### Apple SSD の `smartctl` rc

実機では Apple 内蔵 SSD に対して `smartctl -a disk0` が
`Error Information Log` の取得失敗で **rc=4** を返しても、本体の SMART/NVMe 情報は読めた。

よって:

- **rc != 0 を即「SSD異常」にしない**
- rc=4 のみで、必要な欄が正常に読めている場合は `GetLogPage failed` を故障扱いしない
- `smartctl` の exit status は bit field として解釈し、少なくとも bit 3 (`8`: DISK FAILING) は異常として扱う
- stdout の必要フィールドと exit status の双方から判定する
- 未知の組み合わせは判定不能へ倒す

### 3. APFS の論理整合性 — 別セクション、明示実行のみ

SSD の物理診断とは別に、Data volume の論理整合性を検査する入口を置く。

候補:

```sh
diskutil verifyVolume /System/Volumes/Data
```

これは数分かかる可能性があり I/O も重いので、**タブを開いただけでは絶対に走らせない**。

実装前に実機で次を測る:

- sudo が必要か
- 所要時間
- stdout / stderr / exit code
- 正常時 / 異常時の安定した判定可能箇所
- 実行中に cancel したときの挙動
- mounted Data volume に対して安全に実行できる範囲

doctor の原則として sudo を使わない。
sudo が必要、またはオンライン検証を自動実行する安全性に確信が持てない場合は、
**doctor 自身では実行せず、コピー可能な手動コマンドだけ提示する**。

`diskutil repairVolume` / `fsck_apfs` は実行しない。

## 状態の語彙

SSD タブでは次を区別する。

- `ok` — 検査でき、既知の異常条件に該当しない
- `注意` — 故障ではないが確認対象
- `異常` — 既知の異常条件に該当
- `判定不能` — command 不在 / timeout / parse failure / 権限不足
- `未検査` — 明示検査をまだ実行していない

**判定不能・未検査を緑にしない。**

特に画面全体で次を区別する:

```text
SMART: ok / APFS: 未検査
SMART: ok / APFS: 異常
SMART: 判定不能 / APFS: ok
```

`SMART: ok` だけを根拠に「ディスク正常」と総括しない。

## キャッシュ

SMART 詳細と APFS 検査は明示実行なので、直近結果を時刻付きで保持してよい。

表示例:

```text
SMART detail: 2026-09-29 01:10 に検査
APFS:         未検査
```

既存 doctor snapshot に混ぜるか SSD 専用 snapshot にするかは実装時に決める。
ただし、古い結果には必ず経過時間を出し、現在値のように見せない。

## セキュリティ / プライバシー

- `smartctl -a` の raw stdout をそのまま保存・表示しない
- `Serial Number` を snapshot / UI / clipboard に含めない
- 必要なフィールドだけ parse して構造体へ入れる
- shell 文字列を組み立てず argv で実行する
- timeout / context cancel を持つ
- doctor 終了時に子プロセスを残さない
- sudo を実行しない
- repair を実行しない

## 実装候補

`src/doctor` に SSD 診断用 package を追加し、glogx 側は結果の描画に徹する。

候補:

```text
src/doctor/ssd/
  diskutil.go
  smartctl.go
  apfs.go
  report.go

src/glogx/
  doctor_ssd.go
  doctor_view.go
  doctor_keys.go
```

`doctor_view.go`:

- `tabSSD` を追加
- `numDoctorTabs` を 4 → 5
- タブごとの cursor 保存を維持
- scan state は既存 Disk / Service / Brew / Docker と独立させる
- `scanning()` が「明示検査をまだ走らせていない SSD」を永遠に scanning 扱いしないよう注意する

SSD の明示検査は通常スキャンと state machine を分ける。
`nil = 走査中` という既存 convention をそのまま使うと「未検査」と区別できないため、
SSD 側は `unrequested / running / done / failed` のような明示状態を持つ。

## 非ゴール

- SSD の自動修復
- APFS の自動修復
- sudo の実行
- Apple Diagnostics の代替
- SSD の残り寿命を「あと N 年」と予測すること
- 容量掃除機能を SSD タブへ移すこと
- 外付け SSD 全台への対応を初回から行うこと。まず内蔵起動ディスクを対象にする

## 受け入れ条件

- [ ] doctor に `SSD` タブが追加され、既存 4 タブの操作を壊さない
- [ ] Disk タブ（掃除）と SSD タブ（健康）が明確に分離されている
- [ ] `diskutil info disk0` の SMART Status を表示できる
- [ ] `smartctl` 詳細検査は明示操作でだけ走る
- [ ] `smartctl` が無い場合に `判定不能` とし、ok に丸めない
- [ ] Apple SSD の既知の rc=4 / `GetLogPage failed` だけで異常判定しない
- [ ] `Critical Warning` / `Media and Data Integrity Errors` / `Available Spare` を構造化して判定する
- [ ] `Percentage Used` / `Data Units Written` / `Power On Hours` / `Unsafe Shutdowns` を表示できる
- [ ] `Serial Number` を UI / cache / clipboard に出さない
- [ ] APFS 整合性を SMART と別項目として扱う
- [ ] APFS 検査はタブを開いただけでは走らない
- [ ] `repairVolume` / `fsck_apfs` / sudo を実行するコード経路が無い
- [ ] timeout / cancel 後に `smartctl` / `diskutil` の子プロセスが残らない
- [ ] `未検査` / `判定不能` / `ok` / `注意` / `異常` がテストで区別される
- [ ] status を 1 つ潰す変異（例: 判定不能→ok、integrity errors 無視、rc=4 即異常）がテストで red になる
- [ ] `docs/macos-health-check.md` の SSD の配置説明を、このタブを正本とするよう更新する

## 着手前に再確認すること

この issue は 2026-09-29 時点の現コードと `docs/macos-health-check.md` の実測を元にした。
実装開始時に以下を再確認する。

1. `doctor_view.go` のタブ数・キー割当が変わっていないか
2. `diskutil info disk0` / `smartctl -a disk0` の現在の出力
3. `diskutil verifyVolume /System/Volumes/Data` の sudo 要否・所要・cancel 挙動
4. smartctl の exit status bit の扱い
5. 既存 snapshot へ SSD の明示検査結果を混ぜても stale 表示を作らないか

## 反証レビュー

この ChatGPT connector 環境には repo の規約が要求する codex / read-only subagent の実行口が無いため、
**外部の反証レビューは未実施**。この仕様は「反証済み」と扱わない。
着手時に現コード・実機出力との突き合わせを行い、P1/P2/P3 の反証レビューを通してから実装へ進む。

## 関連

- `docs/macos-health-check.md` — SSD / smartctl / APFS 検査候補の実機調査
- [issue 148](done/148-feat-glogx-doctor-disk-diagnosis.md) — 現在の doctor / Disk タブの起点
- [issue 500](done/500-bug-macos-kernel-zone-leak-from-tmux-clients.md) — メモリリークから kernel panic に至った調査
- `src/glogx/doctor_view.go` — doctor のタブ / state / 描画
- `src/glogx/doctor_keys.go` — doctor の行キー
- `src/doctor/README.md` — doctor の判定・安全性の既存契約
