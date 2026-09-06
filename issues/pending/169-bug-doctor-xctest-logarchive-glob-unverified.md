# 169 bug: `xctest-logarchive` の glob が「実測」と書かれているが実測の記録が無く、黙って 0 件になりうる

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](../done/163-audit-doctor-implementation-red-team.md) (体 1) / [issues/148](../done/148-feat-glogx-doctor-disk-diagnosis.md) (「敵対的レビュー 2 回目」の xctest-logarchive P2)

## 対象

`src/doctor/disk/catalog.go` の `xctest-logarchive` エントリの Paths と、その直近コメント
(「実測の名前: `XCTestTesting.<uuid>.logarchive` / `xctest-*.logarchive`」)

## 何が起きるか

前回の敵対的レビューで、glob `/private/var/tmp/*.logarchive` が「人が `log collect` で採った証跡」にも当たる P2 を
`XCTest*` / `xctest*` に絞って直した。しかし**絞った後の名前が実測されていない**。

裏取りの結果 (体 1):

- issue 148 の記録は `/var/tmp/*.logarchive` が 1.8GB あったという**サイズだけ**で、ファイル名が残っていない
- 修正 commit (824b863) の diff にも測定記録が無い
- Xcode 26 の全 framework / PlugIns / usr/bin を `grep -a` しても `XCTestTesting` を含むバイナリは **0 件**
  (spindump 側は `%@-Spindump.txt` / `Hang (Spindump).txt` が XCTHarness に在る)
- `mdfind` と Trash にも現物が無い

名前が違えば**この検出項目は黙って 0 件**になる (false negative)。P2 の修正が効いているかを誰も確認できない状態。

## 対応案

1. コメントの「実測の名前」を「**未実測**」に直す (今そこに書かれているのは推定であって実測ではない)
2. 次に `xcodebuild test` を回した直後に `ls -la /private/var/tmp/*.logarchive` で実名を採り、glob を確定する
3. 確定するまでは、検出 0 件を「候補なし」と表示するのではなく、この項目だけ「未検証の検出項目」と分かる形にする案も検討する
   (`sinking silently の禁止` = issue 148 2 章の規律)

## 再開の trigger

`xcodebuild test` を回す作業が入ったとき (iOS/macOS アプリ側の作業)。回した直後に上記 2 を実行する。

## 受け入れ条件

- [ ] コメントの主張が実測の有無と一致している
- [ ] 実名を採った後、glob がその名前に当たることを fixture テストで固定する

## 対応 (2026-09-03) — 対応案 1 を実施し、2 / 3 は実測待ちで `pending/` へ

**主張は成立する。** 裏取りを再確認した:

- `issues/148` の Tier 2 表 (`148-feat-glogx-doctor-disk-diagnosis.md:142`) が記録しているのは
  `/private/var/tmp/*.logarchive` という**パターンだけ**で、ファイル名の実測は無い
- 修正 commit `824b863a` の diff にも測定記録は無い
- 実機に現物が無い (`ls /private/var/tmp/*.logarchive` → no matches。2026-09-03 再確認)

### 1 を実施: コメントの主張を実測の有無に合わせた

`src/doctor/disk/catalog.go` の `xctest-logarchive` のコメントから「実測の名前」という記述を削り、
**「この glob 自体が未実測」**であることと、確定手順 (`xcodebuild test` の直後に `ls -la` で実名を採る) を書いた。
コード直近に残すのは [`pending-issue-rationale-in-code.md`](../../_claude/rules/pending-issue-rationale-in-code.md) の規律
(issue は移動するがコードは現場に残り、改修者が該当行を編集する瞬間に必ず目に入る)。

### 2 / 3 は実測待ち → `issues/pending/`

- **2 (実名を採って glob を確定)** は `xcodebuild test` を回さないと測れない。この repo には iOS/macOS アプリが
  無く、シミュレータでの動作確認は封印されている ([`no-ios-simulator-verification.md`](../../_claude/rules/no-ios-simulator-verification.md))
- **3 (未検証の検出項目と分かる形にする案)** は UI の語彙を増やす判断で、2 の結果次第で不要になりうる
  (実名が判れば「未検証」ではなくなる)。2 の前に UI を増やすのは早い

**再開の trigger は据え置き**: iOS/macOS アプリ側で `xcodebuild test` を回す作業が入ったとき。
そのとき `ls -la /private/var/tmp/*.logarchive` を実行して実名を採り、glob を fixture テストで固定する。
コード側のコメントに同じ手順を書いたので、issue が見つからなくても手順は失われない。

## 2026-09-03 の追加調査 — 静的探索は尽きた + 同型が 1 件見つかった

### 静的には確定できないことが分かった (同じ grep を繰り返さないための記録)

起票時の裏取りは「**推測した名前** (`XCTestTesting`) で Xcode を grep して 0 件」で止まっていた。
**名前を作っている側**を探したところ、確定手段が構造的に無いことが分かった:

- 生成の入口は `XCTAutomationSupport` の
  `collectLogArchiveWithStartDate:outputPath:withReply:` (iPhoneOS / WatchOS / AppleTVOS /
  XROS / WatchSimulator の各 platform に同名で在る)。**`outputPath` は呼び出し側が渡す**ので、
  生成側のバイナリに名前のリテラルは無い
- `.logarchive` のリテラルを Xcode 全体 (Platforms / Library/PrivateFrameworks /
  Contents/Frameworks / SharedFrameworks / usr) の Mach-O から拾うと、当たるのは
  `logdump` と `LoggingSupportHost` の**拡張子検査**だけ
  (`An archive must have extension .logarchive` / `File name does not end with .logarchive (%@)`)
- 走査したのは XCTest 系 68 バイナリ + 上記ディレクトリ配下の全 Mach-O (Xcode 26.3)

→ **実行時に採る以外に確定手段が無い**。再開の trigger は据え置き。

### 同型: `xctest-spindump` も同じ推測を共有していた

`catalog.go` の `xctest-spindump` は `/private/var/tmp/XCTestTesting.*.spindump.txt` を使うが、
**その `XCTestTesting.` 接頭辞こそが未確認のもの**だった。実測:

```
grep -rl --binary-files=text 'XCTestTesting' <Xcode>/Contents/Developer/Platforms/MacOSX.platform
  → 0 件 (Xcode 26.3)
ls -la /private/var/tmp/ | grep -i 'xctest|logarchive|spindump'
  → 無し (var/tmp の全体は 5 エントリ)
```

起票時の裏取りは「spindump 側は `%@-Spindump.txt` / `Hang (Spindump).txt` が XCTHarness に在る」と
**書式の方**を確認していたが、**接頭辞は確認していなかった**。この issue は logarchive だけを
対象にしていたので、同じ推測を持つ隣のエントリが視野の外に落ちていた
(規範: `~/.claude/CLAUDE.md`「直したバグは同じ間違いが別の場所にもある前提で grep する」)。

**対応**: `catalog.go` の `xctest-spindump` にも未実測である旨のコメントを入れた。
実名を採るときは**2 エントリまとめて**確定する。

### 受け入れ条件の追記

- [ ] 実名を採るとき、`xctest-logarchive` と `xctest-spindump` の**両方**の glob を確定する

## UI 側 (glogx) にも適用済み (2026-09-03)

対応案 3「検出 0 件を『候補なし』と表示せず、未検証と分かる形にする」は **CLI・UI とも適用済み**。

| 側 | commit | 実装 |
|---|---|---|
| doctor module | `7bb5137d` | `Entry.Unverified` を新設。`disk.Format` が 0 件でも畳まず `🔎 未検証` を出す |
| glogx (画面) | `ecd26414` | `diskSection` / `doctorRiskMark` に同じ規律。CLI と語彙を揃えた |

一時 `doctor_view.go` が凍結されていたため CLI 側だけ先に入り、退避しておいた patch を
凍結解除後 (見直し commit `40e0d7eb` の上) に当てた。目印にしていた 3 箇所
(0 件の畳み込み / `Recover` を出す位置 / `doctorRiskMark` の `StatusOK` 分岐) は
構造が変わっておらず、そのまま当たった。

### 実機での出力 (diskdoctor)

```
       0B  🔎 未検証  XCTest ログ (/var/tmp/*.logarchive)
           0 件ですが「候補なし」ではありません: ファイル名が未実測 (issue 169)

       0B  🔎 未検証  XCTest spindump
           0 件ですが「候補なし」ではありません: ファイル名が未実測 (issue 169)
```

### テストと変異

**CLI と UI を同じ fixture で突き合わせる**形にした (同じデータから同じ結論を描くので、
片方のテストだけではもう片方を 1 mm も守らない。規範: `mutation-verify-new-tests.md`)。

| テスト | 場所 |
|---|---|
| `TestFormatKeepsUnverifiedEntryWithZeroItems` | `src/doctor/disk/report_test.go` |
| `TestDoctorUnverifiedEntryMatchesCLI` | `src/glogx/doctor_view_test.go` (CLI と UI 両方の出力を見る) |

変異 5 本すべてで red:

| 変異 | 結果 |
|---|---|
| CLI: 未検証でも 0 件なら畳む形に戻す | red |
| CLI: マークを `✅ 安全` に戻す | red |
| CLI: 説明行を消して `Recover` を出す | red |
| **UI だけ**畳み込みを元に戻す | red (**UI: 3 件のみ**。CLI 側の assert は無傷) | 
| **UI だけ**マークを `✅ 安全` に戻す | red (**UI: 2 件のみ**) |

下 2 本で CLI 側の assert が落ちなかったことが、**このテストが両側を区別できている**証拠。

## この issue に残っている作業

実名の実測だけ。**`xcodebuild test` を回した直後に
`ls -la /private/var/tmp/*.logarchive` と `*.spindump.txt` を採り、2 エントリの glob を確定する。**
確定したら `Entry.Unverified` を空にすれば、この 2 行は自動的に畳まれる側へ戻る。

⚠️ 上の「静的探索は尽きた」節のとおり、**Xcode を読んでも名前は分からない**。同じ grep を繰り返さないこと。

> 🚨 反証レビューの台帳 (207) は 2026-09-03 に閉じた。**この issue を pending から戻すときに、
> この issue だけ反証レビューを通すこと** (163 の受け入れ条件から引き継いだ規律)。
