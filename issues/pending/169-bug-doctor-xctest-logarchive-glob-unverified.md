# 169 bug: `xctest-logarchive` の glob が「実測」と書かれているが実測の記録が無く、黙って 0 件になりうる

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 1) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「敵対的レビュー 2 回目」の xctest-logarchive P2)

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
コード直近に残すのは [`pending-issue-rationale-in-code.md`](../_claude/rules/pending-issue-rationale-in-code.md) の規律
(issue は移動するがコードは現場に残り、改修者が該当行を編集する瞬間に必ず目に入る)。

### 2 / 3 は実測待ち → `issues/pending/`

- **2 (実名を採って glob を確定)** は `xcodebuild test` を回さないと測れない。この repo には iOS/macOS アプリが
  無く、シミュレータでの動作確認は封印されている ([`no-ios-simulator-verification.md`](../_claude/rules/no-ios-simulator-verification.md))
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

## UI 側 (glogx) の差分は未適用 — 適用待ちのパッチをここに退避 (2026-09-03)

対応案 3「検出 0 件を『候補なし』と表示せず、未検証と分かる形にする」は
**doctor module 側 (`src/doctor/disk/`) だけ適用済み**で、**glogx の画面側は未適用**。

理由: `src/glogx/doctor_view.go` は別マシンで全体見直し中のため、ユーザーの指示で変更を停止した
(2026-09-03)。合図が出たら下のパッチを当てる。当てると CLI と同じく、未実測の 2 エントリが
候補 0 件でも行として残り `🔎 未検証` が付く。

⚠️ **当てる前に見直し後の実装と突き合わせること**。行番号ではなく
`diskSection` の 0 件畳み込みと `doctorRiskMark` の `StatusOK` 分岐を目印にする。

```diff
diff --git a/src/glogx/doctor_view.go b/src/glogx/doctor_view.go
index 7b8d7f61..586ddc66 100644
--- a/src/glogx/doctor_view.go
+++ b/src/glogx/doctor_view.go
@@ -755,7 +755,12 @@ func (v *doctorView) diskSection(o doctorRenderOpts) []doctorRow {
 	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].Size > sorted[b].Size })
 	shown := 0
 	for _, r := range sorted {
-		if r.Status == disk.StatusOK && len(r.Items) == 0 && len(r.Failures) == 0 {
+		// 候補 0 件の行は畳む。⚠️ ただし **検出条件そのものが未実測のエントリは畳まない**
+		// (issue 169 / 207)。畳むと「名前が違って 1 件も当たらなかった」が「候補なし = きれい」と
+		// **同じ見え方**になり、探せていないことが画面から永久に消える (false green)。
+		// 実測で名前が確定して Entry.Unverified が空になれば、この行も自動的に畳まれる側へ戻る。
+		if r.Status == disk.StatusOK && len(r.Items) == 0 && len(r.Failures) == 0 &&
+			r.Entry.Unverified == "" {
 			continue
 		}
 		shown++
@@ -795,6 +800,13 @@ func (v *doctorView) diskSection(o doctorRenderOpts) []doctorRow {
 			rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "           "+r.Reason)})
 			continue
 		}
+		// 未実測のエントリで 0 件のときは、Recover (「消しても再生成されます」) を出さない。
+		// 消す対象が 1 件も無いのに復元方法を出すと、**検出できている**ように読めるため。
+		if r.Entry.Unverified != "" && len(r.Items) == 0 {
+			rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim,
+				"           0 件ですが「候補なし」ではありません: "+r.Entry.Unverified)})
+			continue
+		}
 		advice := r.Entry.Recover
 		if newest := doctorNewest(r); !newest.IsZero() {
 			advice += fmt.Sprintf("。最終更新 %s (%d日前)", newest.Format("2006-01-02"), int(o.now.Sub(newest).Hours()/24))
@@ -898,6 +910,12 @@ func doctorRiskMark(r disk.Result) (string, string) {
 	case disk.StatusFailed:
 		return "❓ 走査できず", ansiDim
 	case disk.StatusOK:
+		// 検出条件が未実測のエントリで候補 0 件のとき。**リスク記号を出さない**:
+		// 「✅ 安全」は「調べたうえで安全」の意味なので、調べられていない行に出すと嘘になる。
+		// 走査自体は成功しているので「❓ 走査できず」とも違う (記号を分けて区別する)。
+		if r.Entry.Unverified != "" && len(r.Items) == 0 {
+			return "🔎 未検証", ansiDim
+		}
 	}
 	switch r.Entry.Risk {
 	case disk.RiskSafe:
```

### 当てた後にやること

- [ ] glogx 側にも同じ主張のテストを足す (doctor module 側は
      `src/doctor/disk/report_test.go:TestFormatKeepsUnverifiedEntryWithZeroItems`。
      変異 3 本で red 確認済み: 畳みに戻す / マークを ✅ 安全 に戻す / 説明行を消して Recover を出す)
- [ ] CLI と UI で同じ入力から同じ結論が出ることを確かめる
      (規範: `mutation-verify-new-tests.md`「同じ判定を 2 箇所で別実装していないか」)
