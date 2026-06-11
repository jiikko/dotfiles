# UI 動作確認に osascript / AppleScript を主役として使わない

## ルール

- **Claude が macOS / iOS アプリの UI 動作確認をするとき、osascript (AppleScript / System Events) を検証の主役として使うことを禁止する**
- UI の動作確認は **XCTest / XCUITest ベース** で行う。標準手順:
  1. UI テストしたい画面要素に `accessibilityIdentifier` を追加する
  2. XCTest / XCUITest でその ID を使って **存在確認** する
  3. クリック (タップ) 後の **状態変化** をテストする
  4. `xcodebuild test` で確認する
  5. osascript は使わない
- osascript が許されるのは **起動・終了・軽い補助操作** (アプリの launch / quit / activate 程度) のみ。UI 要素の探索・クリック・状態読み取りを osascript でやらない
- **検証用スクリプト (例: `scripts/verify.rb`) がプロジェクトにあるなら、Claude はそれだけを叩く**。検証方法をその場で考案しない

## なぜこのルールが必要か

osascript は Claude の動作確認手段として構造的に不適切:

- **UI 状態の観測が弱い**: System Events 経由の UI 要素ツリーは取得が不安定で、見たい状態 (表示中のテキスト / 有効・無効 / 選択状態) が取れないことが多い
- **失敗理由が曖昧**: 「要素が見つからない」が *タイミング* なのか *セレクタ間違い* なのか *本当に存在しない* のか区別できない
- **Claude が探索ループに入りやすい**: 失敗理由が曖昧なため「セレクタを変えて再試行 → また失敗 → さらに変えて再試行…」という無限ループに陥り、時間とトークンを浪費する (実際に発生した)

XCUITest はこの 3 点をすべて解決する: `accessibilityIdentifier` による安定したセレクタ、明確な assertion 失敗メッセージ、待機 (`waitForExistence`) の組み込みサポート。

## 採用パターン

### 動作確認の優先順位

| 優先度 | 手段 | 確認できること |
|---|---|---|
| 1 | **ビルド確認** (`xcodebuild build` / `make build`) | コンパイルが通るか |
| 2 | **Unit Test** | ロジックの正しさ |
| 3 | **XCUITest** (`xcodebuild test`) | UI 要素の存在・操作後の状態変化 |
| 4 | **スクリーンショット成果物** (XCUITest の attachment / `xcrun simctl io screenshot`) | 見た目の確認 (人間のレビュー用に残す) |
| 5 | osascript | アプリの起動・終了・activate 等の軽い補助のみ |

### XCUITest の書き方 (標準形)

```swift
func testExportButtonOpensPanel() throws {
    let app = XCUIApplication()
    app.launch()

    // 1. accessibilityIdentifier で要素を特定 (label 文字列に依存しない)
    let exportButton = app.buttons["export-button"]
    XCTAssertTrue(exportButton.waitForExistence(timeout: 5))

    // 2. 操作
    exportButton.click() // iOS なら .tap()

    // 3. 操作後の状態変化を assert
    let panel = app.dialogs["export-panel"]
    XCTAssertTrue(panel.waitForExistence(timeout: 5))
}
```

実装側には `accessibilityIdentifier` を付与する:

```swift
Button("Export") { ... }
    .accessibilityIdentifier("export-button")
```

### 「確認方法を考えさせない」— verify スクリプトへの一本化

**最も安定するのは、Claude に確認方法を都度考えさせないこと。** プロジェクト側に検証の入口を 1 つ用意し (例: `scripts/verify.rb`、`make verify`)、Claude はそれだけを叩く。

- プロジェクトに verify スクリプト / Makefile target があるか **最初に確認する** (`ls scripts/`, `grep verify Makefile`)
- あるなら **それ以外の確認手段を考案しない** (スクリプトの中身がビルド + テスト + スクリーンショットを面倒見てくれる)
- 無いプロジェクトで UI 確認が繰り返し必要になりそうなら、verify スクリプトの作成を **提案する** (勝手に大規模整備はしない)

### 例外 (osascript を使ってよいケース)

- アプリの起動 / 終了 / activate (`osascript -e 'tell application "X" to activate'` 程度)
- ユーザーが「osascript でこれだけ確認して」と **特定アクションを明示** している場合
- XCTest が存在しない・導入不可能な対象 (他社製アプリの挙動確認など) で、かつ軽い 1 回きりの確認

ただし例外ケースでも、osascript の結果が曖昧なまま再試行を重ねない。**2 回失敗したら別の手段 (スクリーンショット / ユーザーへの確認依頼) に切り替える**。

## やること / やらないこと

- ✓ UI 確認の前に `accessibilityIdentifier` を付与し、XCUITest を書く
- ✓ `xcodebuild test` (または `make test`) で機械的に確認する
- ✓ 見た目はスクリーンショットを成果物として残し、人間に見てもらう
- ✓ プロジェクトの verify スクリプト (`scripts/verify.rb` 等) があればそれだけを叩く
- ✓ osascript は起動・終了・軽い補助に限定する
- ✗ osascript / System Events で UI 要素を探索・クリック・状態読み取りする
- ✗ osascript の失敗に対してセレクタや待ち時間を変えながら再試行ループに入る
- ✗ verify スクリプトがあるのに独自の確認手順をその場で考案する
- ✗ accessibilityIdentifier なしで label 文字列マッチに頼った UI テストを書く (ローカライズで壊れる)

## 関連

- `~/dotfiles/_claude/rules/instrument-before-second-fix.md` — 「観測を増やす」思想は共通。osascript は観測手段として精度が低すぎるため、XCUITest という構造化された観測に置き換える
- `~/dotfiles/_claude/rules/escalate-to-forge-after-failed-tries.md` — 確認手段の試行錯誤も 2 回失敗したら手段自体を見直す
- 本ルールの起源: osascript による UI 確認で Claude が探索ループに入り時間を浪費した実例 (2026-06-11)
