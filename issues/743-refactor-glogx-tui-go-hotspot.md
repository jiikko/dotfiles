# 743 (refactor): glogx の tui.go の変更の集中を減らす (update の case を機能ごとのメソッドへ・塊ごとのファイルへ)

> 🚨 **担当中: treefiler の行番号のセッション**（2026-10-12〜）

起票日: 2026-10-12

## 概要

ユーザー依頼 (2026-10-12「tui.go の重さを下げたいのなら、やる意義があるの?」→「issue に書いて、今対応して」)。

`src/glogx/tui.go` (4,186 行) は glogx で突出して変更が集中する。直近 90 日に glogx を触った commit 751 本のうち 262 本 (35%) が tui.go を
触り、2 位の `issues_view.go` (124 本) の 2 倍。hunk を含む関数で数えると `update` (メッセージの振り分け。旧 `Update`) と `handleKey`
(キーの振り分け) が大半で、機能を足すたびに case の本体をここへ書く形になっている。並行セッションが同じファイルでぶつかる場所でもある。

## 既存の判断との関係 (逆転させないもの)

- issue 018 (2026-07-22) は `browseModel` を「新規抽出の価値なし」とし、**CI の取得の状態機械・push のポーリング・PR のキャッシュ・
  lines のメモ化・panel-frame・scroll-glide を別の型へ抜き出すこと**を「再提案しない」と記録した (`commits` / `statuses` / `offset` を
  共有の土台として読み書きし、抜き出しても全消費者への注入し直しになって結合が切れない)。**この判断は変えない**: 別の型は作らず、
  状態は `browseModel` に残す
- `fullscreen.go` は「interface + 順序つきスライスのレジストリ」を採らないと記録している (issue 227。共通化にアダプタが要り、
  毎フレームの確保が増える)。全画面の配線 (enum の switch + exhaustive の lint + `TestFullScreenSurfacesWireEverySite`) は変えない
- 018 の当時 47 個だったフィールドは 67 個に増えたが、増えた分 (全画面・push の演出・CI の待ち) を別の型にする提案もしない

## 対応方針

同じパッケージ・同じ型 (`browseModel`) のまま、次の 2 つだけをする。どちらも挙動を変えない:

1. **Extract Method**: `update` の大きな case の本体を、名前の付いた `browseModel` のメソッドへ抜き出す (`case ciResultMsg: return m.onCIResult(msg)`)。
   `update` は switch の後ろで `return m, nil` に落ちるだけなので、抜き出した本体の終わりに同じ `return m, nil` を置けば意味は変わらない
2. **塊ごとのファイルへ**: tui.go の中で連続している機能の塊を、メソッドのまま別ファイルへ移す (CI の取得とポーリング / push・pull と演出・
   再読み込み / コミットのパネルと重ねる板 (job の詳細・PR・diff) / 全画面への振り分け / 描画)。抜き出した case のメソッドも同じファイルへ

`~/.claude/rules/verify-design-intent-before-refactor.md` の 3 問:

- 責務の分離: 機能の変更 (CI・push・パネル) が tui.go に波及しなくなる (振り分けの 1 行を除く)
- 探すコスト: 1 つの機能の処理 (メッセージの受け取りと補助) が 1 ファイルにまとまる。今は `update` の case と補助のメソッドが 4,000 行の中に散っている
- 参照範囲: 同じパッケージなので名前の可視性は変わらない (これは改善しない、と明記しておく)

測り方: 着手後の一定期間で「glogx の commit のうち tui.go を触る割合」(着手前 35%) と `update` / `handleKey` の行数。

## 着手前に潰す罠 (tui.go という名前に掛かっている検査)

- `.golangci.yml` の forbidigo「`time.Now` / `time.Since` を直接呼ばず `timeNow` を通す」は `path-except: tui\.go` で **tui.go にだけ**掛かる。
  push の演出 (issue 038 の起源) を外へ出すと規律が黙って外れるので、新しいファイルも対象に入れ、変異 (新しいファイルで `time.Now()`) で赤を確かめる
- `fullscreen_test.go` (`viewLines`) と `hint_registry_gate_test.go` (`hintLine`) は `tui.go` を名前で parse する (見つからなければ赤で止まる)。移した先を読む
- `waitdelay_discipline_test.go` の `execCmdAllowlist` はファイル名 `tui.go` をキーに持つ (job ログを nvim で開く前景実行)
- `.github/workflows/doctor.yml` の paths filter が `src/glogx/tui.go` を見る (doctor への振り分けの配線)。移した先のファイルを足す
- パッケージ変数は動かさない (ファイルをまたぐと初期化の順序が変わりうる)。動かすのは関数・メソッドだけ

## 進捗

- [ ] 設計のレビュー
- [ ] Extract Method と塊ごとのファイルへの移動 (テスト・lint・等価の確認)
- [ ] tui.go に掛かる検査の張り替えと変異での確認
- [ ] CLAUDE.md の地図の更新
