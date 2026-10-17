# 818 (docs): Swift で、別の thread から呼ばれる callback を actor に属する型の中の closure で書かない — rule にするか

起票日: 2026-10-17

## 概要

screen-share-app の retro 808 の提案 2 を切り出した (ユーザーが「issue にする」を選んだ。2026-10-17)。

**起きたこと**: macOS アプリで ⌘Tab を相手へ送るための event tap (`CGEvent.tapCreate`) を、main thread から専用の thread へ移した。
tap の C の callback を `@MainActor` の型のメソッドの中の closure で書いていた。closure は main actor に属するので、
別の thread から呼ばれた瞬間に Swift の実行時の検査で落ち、**起動直後に SIGTRAP**。単体テストは tap を張らないので緑のままだった。
callback をファイルの外側の関数 (actor に属さない場所) に移して直した (screen-share-app の `SystemShortcutCapture` の `tapCallback`)。

## 一般形 (rule の案)

- `@MainActor` (や default isolation が MainActor の module) の型の中で、**別の thread から呼ばれる callback** (C の関数ポインタ・GCD の queue の handler・
  Network.framework の handler・`CFRunLoop` の別 thread の source) を closure で書かない。actor に属さない場所 (ファイルの外側の関数・`nonisolated`) に置き、
  main の状態に触るときは明示的に main へ渡す
- その callback が呼ばれる thread を変えた直後は、**実物を起動して** callback の経路を 1 回通す (単体テストはその経路を通らないことが多い)

## 決めること

- 新しい rule にするか、既存の rule への追記にするか。Swift の並行性の rule は今 `_claude/rules/` に無い (`no-concurrent-spm-build-during-xcodebuild.md` は build の並行の話で別)。
  発動点 (「actor に属する型の中で、別 thread から呼ばれる callback を書く瞬間」) は既存のどの rule とも違うので、追記先は見当たらない
- `paths:` で Swift のファイルを読んだときだけ読み込む形にするか (常時読み込みの合計の上限があるため)
- 起票時点では 1 回の事例だけ。もう 1 回踏むまで待つ、も選択肢

## 関連

- screen-share-app の issue 808 (retro)・`Sources/SSAKit/SystemShortcutCapture.swift` (`tapCallback`)

## 起票時の反証レビュー

省略した (中身は rule にするかの検討で、コードの事実の主張は screen-share-app の `tapCallback` の 1 か所だけ)。反証されていない記述として扱う
