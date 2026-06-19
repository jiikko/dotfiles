# UI 動作確認に osascript / AppleScript を主役として使わない

## ルール

- **macOS / iOS アプリの UI 動作確認に osascript (AppleScript / System Events) を検証の主役として使うことを禁止する**
- UI の動作確認は **XCTest / XCUITest ベース** で行う。標準手順:
  1. UI テストしたい画面要素に `accessibilityIdentifier` を追加する
  2. XCTest / XCUITest でその ID を使って存在確認する (`waitForExistence`)
  3. クリック (タップ) 後の状態変化を assert する
  4. `xcodebuild test` で確認する
  5. osascript は使わない
- osascript が許されるのは**起動・終了・activate 等の軽い補助操作のみ**。UI 要素の探索・クリック・状態読み取りを osascript でやらない
- **検証用スクリプト (例: `scripts/verify.rb`, `make verify`) がプロジェクトにあるなら、Claude はそれだけを叩く**。検証方法をその場で考案しない

## なぜ

osascript は Claude の動作確認手段として構造的に不適切 (探索ループで時間を浪費した実例あり、2026-06-11):

- **UI 状態の観測が弱い**: System Events の UI 要素ツリーは不安定で、見たい状態が取れないことが多い
- **失敗理由が曖昧**: 「要素が見つからない」がタイミングなのかセレクタ間違いなのか本当に無いのか区別できない
- **探索ループに入りやすい**: 失敗理由が曖昧なため「セレクタを変えて再試行 → また失敗 → …」の無限ループに陥る

XCUITest は `accessibilityIdentifier` による安定セレクタ・明確な assertion 失敗メッセージ・組み込みの待機サポートでこの 3 点を解決する。

## 動作確認の優先順位

| 優先度 | 手段 | 確認できること |
|---|---|---|
| 1 | ビルド確認 (`xcodebuild build` / `make build`) | コンパイルが通るか |
| 2 | Unit Test | ロジックの正しさ |
| 3 | XCUITest (`xcodebuild test`) | UI 要素の存在・操作後の状態変化 |
| 4 | スクリーンショット成果物 (XCUITest attachment / `xcrun simctl io screenshot`) | 見た目 (人間のレビュー用に残す) |
| 5 | osascript | 起動・終了・activate 等の軽い補助のみ |

> **iOS の場合**: 優先度3の `xcodebuild test`（XCUITest）はシミュレータを使うため [`no-ios-simulator-verification.md`](no-ios-simulator-verification.md) が優先し、その**実行はユーザーに委ねる**（osascript も使わない）。iOS で自発的にやってよい範囲・macOS の扱いは同ルールを一次情報とする。

## 「確認方法を考えさせない」— verify スクリプトへの一本化

最も安定するのは、確認方法を都度考えないこと。

- プロジェクトに verify スクリプト / Makefile target があるか**最初に確認する** (`ls scripts/`, `grep verify Makefile`)
- あるなら**それ以外の確認手段を考案しない**
- 無いプロジェクトで UI 確認が繰り返し必要になりそうなら、verify スクリプトの作成を**提案する** (勝手に大規模整備はしない)

## 例外 (osascript を使ってよいケース)

- アプリの起動 / 終了 / activate 程度
- ユーザーが「osascript でこれだけ確認して」と特定アクションを明示している場合
- XCTest が導入不可能な対象 (他社製アプリ等) での軽い 1 回きりの確認

例外ケースでも曖昧な失敗のまま再試行を重ねない。**2 回失敗したら別の手段 (スクリーンショット / ユーザーへの確認依頼) に切り替える**。

## やること / やらないこと

- ✓ `accessibilityIdentifier` を付与して XCUITest を書き、`xcodebuild test` で機械的に確認する
- ✓ 見た目はスクリーンショットを成果物として残し、人間に見てもらう
- ✓ verify スクリプトがあればそれだけを叩く
- ✗ osascript / System Events で UI 要素を探索・クリック・状態読み取りする
- ✗ osascript の失敗に対してセレクタや待ち時間を変えながら再試行ループに入る
- ✗ accessibilityIdentifier なしで label 文字列マッチに頼る (ローカライズで壊れる)

## 関連

- [`instrument-before-second-fix.md`](instrument-before-second-fix.md) — osascript は観測手段として精度が低すぎるため、XCUITest という構造化された観測に置き換える
- [`escalate-to-forge-after-failed-tries.md`](escalate-to-forge-after-failed-tries.md) — 確認手段も 2 回失敗したら手段自体を見直す
