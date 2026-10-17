# 817 (refactor): glogx の main を薄くする (API を絞った package の境界)

起票日: 2026-10-17

## 概要

glogx は `package main` に約 6 万行 (非テストの 62 ファイル・テストの 94 ファイル。issue 742 の 2026-10-17 の数え直し) が
並んでいた。Go では main を起動と配線だけの薄いものにし、本体を `internal/` などの package に置くのが一般的なので、
**設計の理由で** main を薄くする。issue 742 は「ビルドキャッシュを減らす」を動機に画面ごとの分割を始めたが、実測で
キャッシュへの効き目は小さく (下)、キャッシュの問題は issue 816 (-trimpath) で扱う。この issue は分割を設計として続ける。

## 742 で済んでいること

- `internal/term` (描画の土台)・`internal/app` (画面が共有する状態と道具)・`internal/doctorview` (doctor 画面) を出した
- doctor のテストは 3 か所に置き分けた: 画面の中を見るテスト (`package doctorview`)・公開面だけで書けるテスト
  (`*_ext_test.go` = `package doctorview_test`)・browseModel と組み合わせるテスト (main。約 20 本)。共有のテスト部品は
  `internal/doctorview/doctortest` (本番からの import は depguard が止める)
- 移設で外れかけた検査の直し方と罠は issue 742 の進捗と retro 815 にある

## 742 のやり方から変えること

- **公開名を機械的に増やさない**。742 は改名を「先頭の大文字化」だけにして等価を機械で確かめられる形にしたが、
  その結果 doctorview は 201 個 (段 3)・term 48 個・app 45 個を公開した (中身のフィールドまで)。Go は package の公開面を小さく
  保つので、画面ごとに「main が画面に頼むこと」(開く・閉じる・キーを渡す・描く・通知を受け取る) を API として設計し、
  それ以外は非公開に戻す
  - 既に出した doctorview の公開面も絞り直す (main と外部テストから実際に使われている名前を数えるところから)
  - 公開名を増やすと lint の検査範囲が狭まる (unparam の定数引数・unused は公開名を見ない。742 の記録) ので、絞ることは検査の面でも効く
- テストは検査する package の隣に置く。browseModel が要るテスト (キーの振り分け・トースト・終了経路などの配線) だけを main に残す

## 順番 (742 の設計の続き)

issues → status → usage / ratelimit → filer / lan (filer と lan は browseModel のメソッドを持つので、画面から main へ頼む口を先に作る)。
issues の下準備 (742 で 2026-10-17 に数えた): url_picker は issues と一緒に、watch_chain と dirWatcher は app へ、
editorCommand など 4 関数と runEditorCmd・openURLMsg は extcmd (新設) へ、notice の種類・firstLine・clampIdx・normalizeSpaceKey は app へ。

## 参考: キャッシュへの効き目 (742 の実測。動機ではない)

1 行足したときの GOCACHE の増分 (`./...` を空の GOCACHE で): tui.go 27.5 → 22.5 MB (−18%)、doctor 27.5 → 31.1 MB (+13%)、
土台 (term) 27.5 → 30.8 MB。直近 3 か月の直す頻度で重み付けすると正味 −1 割程度 (直す回数の 8 割が main)。

## 関連

- issue 742 — 段 1〜3 と doctor のテストの置き分けの経緯・実測・罠 (この issue が続きを持つ)
- issue 816 — キャッシュの問題は -trimpath で扱う
- retro 815 — 移設が「名前・パッケージ・可視性」に掛かった検査を外す形
