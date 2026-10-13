# 748 (bug): glogx の issues viewer の書き込み: next から出す移動の失敗で claim が半端に外れる / バナーの front matter の判定が LoadMeta と違う

起票日: 2026-10-13

## 概要

2026-10-13 の監査 (記録は 754) で見つけた、issue ファイルへ書き込む経路の不具合 2 件。どちらも scratchpad の複製で再現テストが FAIL した (main が再実行して確認)。

## 詳細

### 1. next から出す移動が宛先の同名で失敗しても、バナーと目印が外れている (P2)

- 該当: `src/glogx/issues/move.go` の `MoveToSubdir`。next から出る経路は「`clearClaimBanner` → `removeNextLink` → 宛先の決定 → `os.Stat(dest)` で同名を確かめて返す」の順。
  同名の確認が最後なので、失敗して返った時点でバナーも目印も消えている。関数の doc は「この順なら失敗は何も変わっていない」と主張しているが、同名の失敗には当たらない
- 発火条件:
  - symlink の目印で claim した issue を、同名が既にある `done/` か `pending/` へ移す → 「同名のファイルが既にあります」のエラーなのに claim が黙って解除される
  - 旧運用 (next/ に実ファイル) の issue を同じく移す、または直下へ戻す (`subdir=""`) → バナーだけ外れて next/ に残り、`tests/issues/test_next_claims_have_banner.sh` が赤になる
- 再現: 複製上の `TestReproMoveFromNextToDoneConflictLeavesHalfState` (`banner=false link=false`) と `TestReproLegacyNextToDoneConflict`
- 同名以外でも同じ半端が残る (反証レビュー): バナーと目印を外した後に、`GroupKey == ""` の拒否・`os.MkdirAll`・`os.Rename` も失敗しうる。どれで返っても「バナーだけ外れる / claim が外れる」になる
- 直し方: 宛先の確定 (`ContainerDir` と group の判定) と同名の確認を、バナー・目印を外す処理より前へ動かす。`MkdirAll` / `Rename` の失敗ではバナーと目印を戻す (戻せなければ両方をエラーに書く。next へ入る経路の戻しと同じ形)。
  戻さない経路が残るなら doc の「この順なら失敗は何も変わっていない」を直す。同名・group の拒否・rename の失敗で何も変わらないことを回帰テストにする (失敗の注入は 751 の 5 と同じ手段)

### 2. `addBanner` の front matter の判定が `LoadMeta` と違い、698 の 5 と同じ症状が入力の形を変えると残る (P3)

- 該当: `src/glogx/issues/banner.go` の `addBanner` は、`\r` を落とした行が `"---"` に完全一致するときだけ front matter とみなし、閉じも `"---"` だけを、ファイルの最後まで探す。
  `src/glogx/issues/parse.go` の `Issue.LoadMeta` は `TrimSpace` してから比べ、閉じは `---` と `...` の両方を受ける (tuikit の markdown の `skipFrontMatter` も同じ規則)
- 発火条件と結果:
  - (a) `...` で閉じた front matter (本文に `---` なし) → バナーが `---` の前に入り、`Declared` が空になって status 不一致の 🚨 が消える
  - (b) 先頭行が `--- ` (末尾に空白) → (a) と同じ
  - (c) `...` で閉じ、`## 概要` より後ろに本文の `---` がある → バナーが `## ` の後ろに入る。`bannerIndex` が見つけられず、`n` で外すことができない (`changed=false still=true`)。dotfiles の issues なら `tests/issues/test_next_claims_have_banner.sh` も赤になる (front matter を持つ issue は今 0 件)
- 再現: 複製上の `TestReproBannerDotsCloseNoRule` / `TestReproBannerFrontMatterTrailingSpace` / `TestReproBannerDotsCloseRuleAfterH2`
- 現状の影響: 潜在。dotfiles の issues に front matter を持つものは 0 件 (監査体の数え)。他の repo の issues を viewer で開けば当たる
- 直し方: front matter の閉じの行番号を返す関数を `issues` に 1 本置き、`LoadMeta` と `addBanner` がそれを通す (閉じの探索は front matter の範囲に限る)。
  (a)〜(c) を回帰テストにする。閉じの無い `---` の扱い (LoadMeta は全部 front matter、markdown は水平線) もこのとき揃える

## 関連ファイル

- `src/glogx/issues/move.go`・`src/glogx/issues/banner.go`・`src/glogx/issues/parse.go`
- 前回の修正: 698 (5・6 で `addBanner` に front matter の判定を足した)。監査の記録: 754

## 進捗

- [x] 1 宛先の確定・group の拒否・同名の確認を目印とバナーに触る前に置き、その後の失敗 (MkdirAll / Rename / バナーの解除) ではファイルと目印を戻す。
  順序は「目印を外す → 動かす → 動かした先でバナーを外す」(バナーを外した後に戻すと元の文言を復元できないため) — `fix(glogx/issues): next から出す移動の失敗で claim を外さない・front matter の境界を 1 本の判定にする`
- [x] 2 front matter の境界を `isFrontMatterOpen` / `isFrontMatterClose` に寄せ、LoadMeta と addBanner が共有する (同 commit)
- [x] 敵対レビュー (壊す) の P2: 戻しが「走査の後に他のホストが外した目印」まで作り直していた → `removeNextLink` が実際に消したかを返し、消したものだけ戻す。
  P3: BOM 付きの front matter で LoadMeta (無害化した行) と addBanner (生の行) の判定が割れた → 述語の中で無害化する — `fix(glogx): 敵対レビューの 3 件 (...)`
- [x] 敵対レビュー (素通り): rename だけが失敗する経路の戻しと、バナーが front matter の閉じより後ろにあることを検査に足した — `test(glogx): 敵対レビュー (素通り) で緑のままだった変異 4 本を止める`
- [x] spec (`docs/issues-viewer-spec.md` の n の節) を更新
- 変異 (すべて red): MoveToSubdir を旧実装へ / 出る向きの rename の戻しを消す / banner.go を旧実装へ / removed の判定を消す / 述語の無害化を消す / 宛先への rename の戻しを消す / 閉じの探索を j=0 から
- 残す既知の半端: 戻しの rename も失敗する二重失敗 (doc に記載)・戻しの rename が移動中に作られた同名を上書きする TOCTOU・宛先の壊れた symlink を os.Stat が「無い」と読む (どちらも以前からの挙動で未実行。敵対レビューの未確認)
- `make -C src/glogx test` (-race) / `make lint` rc=0。敵対レビュー 3 体 (壊す / 素通り / 回帰。read-only・直列) を通した。
