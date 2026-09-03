# 167 bug: `collectBundleIDs` の走査漏れで、インストール済みアプリのコンテナを孤児にする

起票日: 2026-09-02
重要度: **P1**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 1 / 体 5) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (1 章 Tier 2 / 「敵対的レビュー 2 回目」の orphan-container P1)

## 対象

`src/doctor/disk/guard.go` の `collectBundleIDs` / `containerOwnedByInstalled`、
`src/doctor/disk/scan.go` の `scanEntry` の GuardOrphanApp filter (`id := filepath.Base(p)` でディレクトリ名を bundle id 扱いする箇所)

## 何が起きるか

`~/Library/Containers/<...>` の孤児判定が、実在するアプリのデータコンテナを孤児 (`RiskConfirm` trash) として候補に出す。
④ (削除) の入力になるので、そのまま実装すると**生きているアプリのデータを消す候補**になる。

走査漏れは 3 系統ある (どれも `collectBundleIDs` が bundle id を集めきれない / 名前で突合できないことが原因)。

### (a) UUID 名コンテナ (iOS-on-Mac アプリ) — 実証済み

再現: 実機で `disk.Scan` を orphan-container だけの Catalog で走らせる。

- `~/Library/Containers/0A7CEF49-521F-4A65-95E2-9B8495EA27BB` が orphan と判定される
- 中身は `Data/Library/Preferences/com.jiikko.promise.plist` + `Documents/` で、実体は
  `/Applications/やくそく帳.app` (`Wrapper/Promise.app`, bundle id `com.jiikko.promise`) のデータコンテナ

iOS スタイルのコンテナは**ディレクトリ名が UUID** で、bundle id は `.com.apple.containermanagerd.metadata.plist` にしか無い。
その plist は TCC で読めない (`plutil` が operation not permitted)。`containerExcludePrefixes` の `com.jiikko.` も
`installed[id]` も**ディレクトリ名に対して**評価されるので、この形は構造的に素通りする。

### (b) `Wrapper/` と `Versions/A/Resources/` の Info.plist を読まない — 実コードで確認

`collectBundleIDs` は `<bundle>/Contents/Info.plist` しか読まない。次の 2 つの実在形が集まらない。

- iOS-on-Mac: `X.app/Wrapper/Y.app/Info.plist` (flat 配置)
- framework: `.framework/Versions/A/Resources/Info.plist` (実機に 1Password の Electron Framework 等)

(b) を直しても (a) は名前が UUID なので届かない。両方要る。

### (c) symlink の `.app` を読まない — 実証済み (実機の被害は com.apple. のみ)

`os.ReadDir` の `e.IsDir()` は symlink に対して false を返すので、symlink の `.app` は走査されない。

- 実機の depth 2 の symlink `.app` は `/Applications/Safari.app` と `Utilities/Feedback Assistant.app` の 2 件だけ
  (どちらも `com.apple.` 除外に当たるので現状は無害)
- cask は move インストールなので該当しない
- 偽 AppDirs の probe では、別所への symlink である `Linked.app` の bundle id が集まらず、そのコンテナが孤児判定された (体 5)

## 対応案

1. **fail-closed を足す**: コンテナのディレクトリ名が bundle id の形でない (UUID 正規表現に一致する) ものは
   「判定できず」にして候補から外す。metadata plist が読めるなら `MCMMetadataIdentifier` で突合し、読めなければ外す
2. `collectBundleIDs` に `Wrapper/*.app/Info.plist` と `Versions/*/Resources/Info.plist` を足す
3. `e.IsDir()` ではなく `os.Stat` (symlink 解決後) で dir 判定する。解決先が AppDirs 外でも bundle id は集める

1 が単独で被害を止めるので先に入れてよい。2 と 3 は偽陰性を減らす側。

## 受け入れ条件

- [ ] UUID 名のコンテナが候補に出ない (実機で probe を回して 0 件を確認)
- [ ] `Wrapper/` 形式の bundle id が `collectBundleIDs` に含まれる (fixture テスト)
- [ ] symlink の `.app` の bundle id が含まれる (fixture テスト)
- [ ] 変異検証: 1 の fail-closed を外すと UUID コンテナが候補に戻ることを確認する

## 事前検証 (2026-09-03、実コード)

**(b) と (c) は実コードで確認できた。**

- `collectBundleIDs` (`src/doctor/disk/guard.go:167`) が読むのは `<bundle>/Contents/Info.plist` **だけ**
  (`guard.go:184`)。`Wrapper/*.app/Info.plist` と `Versions/*/Resources/Info.plist` は読まない
- `os.ReadDir` の結果を `e.IsDir()` で絞っている (`guard.go:179`) ので、**symlink の `.app` は走査対象から外れる**
  (`DirEntry.IsDir` は symlink に対して false)
- `containerOwnedByInstalled` (`guard.go:209`) は `id` (= ディレクトリ名) を bundle id として突合するので、
  (a) の UUID 名コンテナは構造的に素通りする

### 実機での裏取り (2026-09-03。issue 207 の残作業として実施)

**UUID 名コンテナは実在し、その持ち主はインストール済みのアプリだった。** 修正が守っている
対象が実在したことの確認になる。

| 確かめたこと | 結果 |
|---|---|
| `~/Library/Containers` の UUID 名 | **1 件** (`0A7CEF49-521F-4A65-95E2-9B8495EA27BB`。全 646 件中) |
| 中身 | `Data/Library/Preferences/com.jiikko.promise.plist` (+ `.com.apple.containermanagerd.metadata.plist`) |
| 持ち主 | `/Applications/やくそく帳.app` → `Wrapper/Promise.app` (`CFBundleIdentifier` = `com.jiikko.promise`)。**インストール済み** |
| 修正後の `diskdoctor -json` | `orphan-container` の候補 **7 件**。この UUID は**含まれていない** |

候補に残った 7 件 (`LINE.TimelinePreviewService.0` / `.1` / `com.1password.browser-support` /
`com.adobe.Acrobat.Pro` / `com.google.one` / `com.pdfeditor.pdfeditormac` /
`ru.keepcoder.Telegram.TelegramShare`) は起票時の敵対的レビューが「本体不在か旧版の残骸で
**正しい孤児**」と判定した 7 件と一致する。

🚨 **iOS-on-Mac アプリは実際に UUID 名のコンテナを作る**ことが確認できたので、この形は
「起票時にたまたま在った」ものではない。同じ形は他のマシンでも出る。

## 対応 (2026-09-03)

**修正した。対応案 1 / 2 / 3 をすべて実施。**

1. **(a) UUID 名のコンテナを fail-closed** (`disk/scan.go` の GuardOrphanApp filter + `containerIsUndiagnosable`)。
   突合は「ディレクトリ名 = bundle id」を前提にしており、UUID 名は構造的に素通りするので、
   判定できないものとして候補から外す。metadata plist は TCC で読めないため突合の手段が無い
2. **(b) `readBundleID` を切り出し、`Wrapper/*/Info.plist` と `Versions/*/Resources/Info.plist` も読む**
3. **(c) `e.IsDir()` を `os.Stat` に変え、symlink の `.app` も走査する**

### 敵対レビューで判明した「修正が生んだ新しい failure mode」(どちらも P1)

- **symlink 巡回でスキャンが無期限にハングする**。旧コードの `e.IsDir()` は symlink に false を返すので
  **副次的に循環防止を担っていた**。それを外したとき、`bundleMaxDepth` は深さしか縛らず**幅は無制限**
  なので、同じディレクトリを指す symlink を n 個並べると n^8 に膨らむ (実測: n=2 で 67ms / n=3 で 1.32s /
  n=4 は 8 秒でも終わらない)。`~/Applications` はユーザー書き込み可能で、`collectBundleIDs` には
  ctx が無くキャンセルもできない。→ `(dev, inode)` の `seen` で巡回を止めた
  ([`list-masked-failure-modes-before-removing-guard.md`](../_claude/rules/list-masked-failure-modes-before-removing-guard.md)
  の逆パターン: 防御を「外した」のではなく「外れていることに気づかなかった」)
- **`filepath.Glob` 化が `Contents/Info.plist` にも波及し、メタ文字入りのアプリ名で取りこぼす**。
  `MyApp [Beta].app` の `[Beta]` が文字クラスとして解釈され、そのアプリの bundle id が集まらない。
  影響方向は「拾いすぎ (安全側)」ではなく**拾わなすぎ**で、実在するアプリのコンテナが孤児候補に出る =
  **この issue が塞ごうとした症状の再発**。→ 固定パスは Glob を通さず、可変部のあるものだけ
  `escapeGlobMeta(bundle)` (issue 175 で同 package に足した関数) で prefix を escape する

### 変異検証 (9 本すべて red、ケース名ごとの pass/fail まで確認)

UUID fail-closed 除去 (大文字・小文字**両方**のケースが red) / `Wrapper`+`Versions` 除去 / `Wrapper` のみ除去 /
`e.IsDir()` へ戻す / 巡回検出の除去 / `Contents/Info.plist` を Glob 経由へ戻す / `escapeGlobMeta` を外す /
**深さ 1 の重複だけ許す「半端な回帰」** / 巡回検出を消して訪問回数が発散する形。

🚨 巡回検出のテストは当初 10 秒の壁時計で判定していたが、
[`avoid-wall-clock-assertions.md`](../_claude/rules/avoid-wall-clock-assertions.md) に従って
**訪問回数** (`collectVisits`) の上限で判定する形に書き換えた。正常側は実ディレクトリ数ぴったり (9)、
巡回検出なしは 30 秒で 72,979 回に発散するので、閾値をどこに置いても桁で判別できる。
この書き換えによって、レビュワーが「作れなかった」と報告していた**半端な回帰**
(深さ 1 の重複だけ許す形。0.00 秒で終わるので壁時計では素通りする) も検出できるようになった。

🚨 統合テストの小文字 UUID ケースは当初 vacuous だった。同じ値の大文字/小文字は APFS (既定は
大小無視) が同じディレクトリに畳み込むので `got` に現れず、UUID fail-closed を丸ごと外しても
その assert 行は一度も発火しなかった → **別の UUID 値**を使う形に変えた。

### 敵対的レビュー (sonnet / read-only / 2 周)

1 周目 5 観点: 採用 3 (P1 ×2 + vacuous ×1) / 却下 0。上記のとおり。
1 周目で**壊せなかった**もの: UUID 正規表現の緩さ/厳しさ (実機の `~/Library/Containers` 679 件を全件確認し、
UUID 形も bundle id でない名前も 0 件。`com.example.0A7CEF49-...` のような形は正しく false) /
単発の循環 symlink (`os.Stat` が ELOOP を返すので無害) / `Wrapper`・`Versions` の拾いすぎ
(実機で `Versions/*/Resources/Info.plist` は 636 件ヒットするが、`installed` 集合が増えるだけで
孤児判定は非所属判定なので安全側) / 壊れた symlink・権限なしディレクトリ。

2 周目 5 観点: 採用 0 (新たに壊せるものは無し) / 記録 3。

- **記録 (実害なし)**: `readBundleID` が `seen` の重複チェックより前にあり、同じ実体の別名で
  毎回 Info.plist を読み直していた (冪等なので正しさには影響しないが無駄) → `seen` を先に見る形へ変更
- **記録 (現状維持)**: `seen` を AppDirs 間で共有していない。共有すると稀な二重走査を減らせるが、
  inode 衝突が起きたときの被害範囲が 2 root にまたがる。非共有の方が保守的なので現状維持
- **記録 (未実測のリスク)**: `(dev, inode)` の一意性は実機の `/Applications` (7,272 dir) と
  `~/Applications` で重複ゼロを確認したが、**ネットワークホームディレクトリ (smbfs 等が inode を
  合成する既知の問題クラス)** では衝突しうる。実マウントできる SMB 共有が無く再現も反証もできなかった。
  衝突すると正当なディレクトリを巡回と誤認して走査を打ち切る = bundle id の取りこぼし
- 2 周目で**壊せなかった**もの: `escapeGlobMeta` のエッジケース (改行・バックスラッシュ・末尾スラッシュ) /
  大規模ストレス (自己参照 symlink 50 本 + fan-out 20 本で 5.16ms) / 実機の `(dev, inode)` 一意性
