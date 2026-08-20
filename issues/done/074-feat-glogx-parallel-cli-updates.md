# 074 feat: glogx の claude update と codex update を並列実行できるようにする

起票日: 2026-08-21

ユーザー要望 (2026-08-21): 両方の CLI に更新があるとき、`C` と `X` を順に押すと直列に
待たされる。時間がかかるので並走させたい。

## 現状 (コードで確認済み)

`src/glogx/action_modal.go` の状態が**単数**で、同時に 1 つしか走らせられない:

```go
updating     bool   // claude / codex update 実行中 (終了以外のキーを無視)
updateTarget string // updating 中の対象 CLI 名 ("claude" / "codex"。モーダルの題字用)
```

- `running()` / `active()` が `a.updating` を見ており、実行中は「終了以外のキーを飲む」
  → 片方の update 中は `C` / `X` そのものが届かない
- `boxLines` のタイトルは `updateTarget` で claude / codex を出し分ける (1 つ前提)
- `runUpdate(target)` が `updating = true` / `updateTarget = target` を立て、`updateMsg` の
  受信で降ろす

## 設計方針

状態を「対象ごと」に持つ。単なる bool 2 本ではなく set にする (issue 071 が指摘している
「相互排他でない状態を bool の束で持つ」形をこれ以上増やさないため)。

```go
updating map[string]bool   // 実行中の対象 ("claude" / "codex")。空なら update なし
```

- `running()` / `active()` は `len(a.updating) > 0`
- `runUpdate(target)`: **既に同じ target が走っていれば no-op** (npm の同時自己更新は壊れる。
  二重起動の防止は必須)
- `updateMsg` 受信で該当 target を delete。**両方終わるまでモーダルは残す**
- `boxLines`: 走行中の対象を列挙する (1 つなら従来どおりの題字、2 つなら両方の進行を出す)
- `handleKey`: update 中でも `C` / `X` は通す (他のキーは従来どおり飲む)。ここが並列化の本体

## 影響範囲 (grep で確定済み)

| 場所 | 何をするか |
|---|---|
| `action_modal.go` の型定義 / `active()` / `running()` / `runUpdate` / `boxLines` | 単数 → set 化 |
| `tui.go` の `C` / `X` ハンドラ (:1363, :1371 付近) | update 中でも起動を許す |
| `tui.go` の `updateMsg` 受信 (:175 付近の型と処理) | 該当 target だけ降ろす |
| `tui.go` の再起動ダイアログ抑止 (:927, :1832, :1842 付近) | `running()` 経由なので変更不要の見込み。要確認 |
| `action_modal_test.go:24` / `codex_version_test.go:32-50` / `tui_overlay_test.go:846` / `autobuild_test.go:255` | `a.updating = true` / `updateTarget` を直接触るテストの書き換え |

## 不変条件 (壊してはいけないもの)

1. **同じ CLI の二重起動をしない** (npm の自己更新が競合する)
2. **update 実行中は終了できない** — 現行の「完了まで終了できません」を維持する。並列時は
   *両方*終わるまで。`tui.go` のコメントが警告するとおり、ここで終了を許すと update / git を
   kill する経路が開く
3. **再起動ダイアログ (autobuild) を update 中に出さない** — `autobuild_test.go` が固定している
4. 早期リターン (既に latest) では spinner モーダルを光らせない (ユーザー指摘 2026-08-12 の回帰)
5. `active()` と `handleKey` の一致 — `TestActionModalActiveMatchesHandleKey` が固定している。
   「描かれるモーダル」と「キーを受け取るモーダル」がずれると、画面の選択肢が効かない事故になる
   (実例が action_modal.go の doc コメントにある)

## 検証

- 変異検証を必ず通す: (a) 二重起動ガードを外すと red になるテスト、(b) 片方の完了で
  モーダルが閉じてしまう退行を捕まえるテスト、(c) update 中に終了できてしまう退行を
  捕まえるテスト
- `make -C src/glogx test` (-race) と `make -C src/glogx lint`
- 敵対的レビュー: 「並列時に片方の結果でもう片方の状態を壊す経路」「両方失敗したときの
  トースト/結果表示の取り違え」「Ctrl-C 2 回の強制終了が片方だけ cancel する経路」を攻める

## 対応 (2026-08-21, commit 500f729)

`updating map[string]bool` へ変更して並走を実現。敵対的レビュー 2 観点で **実装バグ 2 件**が
出たため設計を変えた (最初の実装は C/X を browseModel へ素通ししていた)。

- **status viewer への漏れ (実害)**: update モーダルが viewer に乗った状態で X が破棄確認を
  立て、update 完了後の y で `git restore` が着弾した。素通しをやめ actionModal が消費する形へ
- **早期リターンが走行中の update を降ろす**: 走行中の C 再押下で「すでに latest」判定が
  `finishUpdate` に到達し、Ctrl-C が通って `claude update` が 2 本走った。`updateMsg` に
  `early` 印を足して実行結果と区別

併せてトリアージ #4 (update の画面で Enter が git push を起動) と、Ctrl-C の判定順で push の
force-quit 脱出口が消える latent も塞いだ。変異検証 14 種すべてで red を確認。

## 残った未対応

- `finishUpdate` の target 検証は入れていない (未知値で「閉じない」/ 空値で「全部閉じる」)。
  現在 `updateMsg` の生成箇所 3 つはすべて固定文字列なので**到達不能**。3 本目の CLI や
  外部から target を組む処理が入ったら検証を足す
- 本来の直し方「描画とキー判定を同一の状態値から導出する」は issue 071 に残置 (今回は
  例外を 1 箇所に閉じ込め、`active()` ⇔ `handleKey` の同値は C/X でも成立させた)
