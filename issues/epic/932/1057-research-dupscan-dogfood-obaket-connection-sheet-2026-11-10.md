# 1057 (research): obaket の接続の sheet の修正で dupscan を使った所見

> 🚨 **担当中: Mac (glogx)**（2026-11-11〜）

起票日: 2026-11-10

所属: [932](932-epic-dupscan-accuracy.md)

## 概要

obaket の接続の編集モーダルで「保存したら閉じる」を直す作業 (dupscan-dogfood) で使った所見。観察の記録で、仕様や設計の主張ではないので反証レビューは省略した。既存の issue との重複照合はしていない。

## 集計

- 摩擦 3 件 (使い方が分かりにくい 1 / 見逃し 1 / 雑音 1)。提案は摩擦の項に 1 行ずつ添えた。
- dupscan を打った回数 24 (def 12 / index 7 / refs 2 / gate 2 / capabilities 1)。識別子の grep に戻した回数 3 (F2 の理由 1 / 習慣で grep した 2。うち 1 回は案内の直後)。
- 役に立った場面 2:
  - `dupscan gate` が、テストに書いた host の写し 2 本 (`TextFieldHost.init` と `SecureFieldHost.init`) を commit 後の検査で止め、1 つの generic な host に寄せた。
  - `dupscan def ConnectionSheetFooter` が型のメンバーを行番号つきで 2,386 byte で返し、save の入口にそのまま行けた。所要は 6.9 秒 (`time` の total)。

## F1 `index -name` に `|` を渡すと、案内なしで 0 件になる

- 場面: `dupscan index -name 'save|Save|dismiss|Dismiss|onSaved|close' -limit 40 shared/Sources/ObaketPresentation/Connections`
- 期待: どれかの語に当たる単位か、`|` は使えないという案内。
- 実際: `当たる単位なし (単位 497)`、rc 0、275 byte。`-name dismiss` は同じ範囲を広げた実行で 4,278 byte 返った。`-name` は部分一致で、`|` を文字として探している (usage の「名前の一部」どおり)。
- その後: 語を 1 つにして引き直した。grep の癖で `|` を書くと「無い」に見える。
- 種類: 使い方が分かりにくい
- 対応: 下の「直したもの」。

## F2 `refs` が closure のプロパティの実装の場所 (呼び出しの引数ラベル) を出さない

- 場面: `dupscan refs onSaved` (`let onSaved: (StorageConnection) async -> Void` の中身を探した)
- 期待: `ConnectionSheetView.swift:245` の `onSaved: { connection in ... }`。保存の後に何が走るかはここに書いてある。
- 実際: 呼び出し 1 件 (`await onSaved(connection)`) だけ、353 byte、rc 0。
- その後: `git grep onSaved` に戻して見つけた。
- 種類: 見逃し
- 提案 (出力の中身): closure の型を持つプロパティの `refs` には、同じ型の初期化の引数ラベルも `[引数ラベル]` として出す。`refs ConnectionSheetFooter` の行には `ConnectionSheetFooter(` が出ていて、呼び出しの位置は分かっている。

## F3 `index -returns` の 1 画面目に、戻り型が完全に一致する fixture が出ない

- 場面: `dupscan index -returns StorageConnection -limit 15 macOS/ObaketTests` (テストの接続の fixture を探した)
- 期待: `StorageFixtures.connection(...)` が先頭付近に出る。
- 実際: 100 件中 15 件で 2,940 byte。先頭は `makeViewModel() -> StorageConnectionsViewModel` (部分一致で当たった別の型)。`StorageFixtures` は `-limit 0` で 29 行目が最初だった。
- その後: 対象のテストの中の既存のプロパティを読んだ。
- 種類: 雑音
- 提案 (精度向上): 戻り型が語と完全に一致する単位 (`?` の有無を除く) を、部分一致より先に並べる。

## 直したもの

- F3: `-returns` を渡したとき、戻り型が語と完全に一致する単位 (末尾の `?` `!` を除く) を部分一致より先に並べる (`BuildIndex`)。obaket の `-returns StorageConnection -limit 15 macOS/ObaketTests` は、先頭が `makeViewModel() -> StorageConnectionsViewModel` と `snapshot() -> [StorageConnection]` だった並びから、全 15 行が完全一致になった。
- F1: `-name` に `|` があって 0 件のとき、語ごとに引き直す案内を stderr に出す。usage にも正規表現ではないと書いた。テストは「`|` で 0 件なら出す / `|` が無い 0 件と当たったときは出さない」。案内の条件を消す変異で赤を確認した。

検証: `make -C src/dupscan test lint` が rc 0。実物の `-name 'dismiss|save'` で案内が出ることを確かめた。root の `make test` は別の重い検査と同時に回して失敗した (`test_claude_bin`・`test_dangling_symlinks`・`test_ratelimit_warn`・`test_git_prompt`・`src/pro-con/ui` の `TestPopupGivesUpOnHungTmux`・`src/ratelimit/usage` の `TestFetch*`)。dupscan の検査は通っていた。単独で回し直すと dangling symlink 以外は通った。dangling symlink は `~/.claude/rules` と `~/.config/nvim` の手元の link の残骸で、この変更とは関係ない。

## 直していないもの

- F2: 出力の形を変えるので、このタスクの範囲では直さず記録した。
