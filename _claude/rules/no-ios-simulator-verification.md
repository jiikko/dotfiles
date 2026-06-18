# iOS シミュレータでの動作確認は封印する（人間の明示指示があるときだけ解禁）

- **iOS シミュレータを使う動作確認（ランタイム DL / device 作成 / boot / `make test` / アプリ起動・UI 操作・スクショ）を自発的に行わない**。トークン消費が激しいため封印する。
- 自発的にやってよいのは **ビルド（コンパイル）確認まで**（`xcodebuild ... build` / `build-for-testing` / `make build` / `make lint`）。テストはコンパイルが通る状態まで書いて止め、実行コマンドを提示してユーザーに委ねる。
- **解禁条件は人間の明示指示のみ**（「シミュレータで確認して」等）。「念のため動かして確認」をしない。macOS アプリは対象外。
- UI 検証手段の方針は [`no-osascript-for-ui-verification.md`](no-osascript-for-ui-verification.md) を参照（osascript でなく XCUITest）。ただし iOS では本ルールが優先し、XCUITest の**実行**はユーザーに委ねる（ビルド確認までは自発的に可）。
