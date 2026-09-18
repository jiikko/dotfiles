# av1ify: 破損ソースを修復しながらエンコードするモードを追加する

起票日: 2026-09-18
種別: feat
状態: pending (ユーザー判断で保留。2026-09-18)

## 背景

手作業で修復したファイルを最終成果物にするのではなく、av1ify 自身が破損ソースから
修復済みの出力を作れるようにしたい (ユーザー方針)。issue 394 (VFR → CFR 正規化) は
この方針で対応済み。残る 2 件の破損パターンを本 issue で扱う。
(ファイル名はプライバシーのため省略。いずれも WMV / ASF ソース)

### ケース A: 末尾パケットの破損

- `packet_frag_size is invalid` (ASF デマルチプレクサ) + 末尾1フレームのデコードエラー
- 手作業の修復: `ffmpeg -err_detect ignore_err -fflags +discardcorrupt -i in -c copy out.mkv`
  (欠落は末尾 0.16 秒のみ)

### ケース B: 全編に散在する映像破損 + 音声タイムスタンプ異常

- WMV3 映像に `corrupt decoded frame` / `concealing N DC, AC, MV errors` が 102 箇所
- 音声 (stream 0) の DTS が非単調増加。5:33〜5:54 付近に集中、他にも数箇所
- ユーザー報告: av1ify (av1c) で「1:14 くらいで映像が止まる」
- 手作業の修復: デコード (concealment 込み) → h264_videotoolbox + AAC へ再エンコード

## 未確認 (着手前に観測する)

- 🚨 ケース B が **av1ify のどこで止まるのか未観測**。「1:14」がソース上の位置か経過時間か
  も不明。推測で直さず、まず元ファイルに現行 av1ify をかけてログを採る
  (instrument-before-second-fix.md)
- ケース A が現行 av1ify で実際に失敗するかも未確認

## 方針案 (未決定)

- 入力側オプション `-fflags +discardcorrupt -err_detect ignore_err`、音声 `aresample=async=1`
- 🚨 **常時有効にするか `--repair` 等の明示オプションにするか** はユーザー判断待ち。
  常時有効だと破損を黙って飲み込むため、既定は明示オプションを推奨

## 残タスク

- [ ] ケース A / B の元ファイルに現行 av1ify をかけて失敗箇所を観測
- [ ] 常時 / オプションの判断 (ユーザー)
- [ ] 実装・テスト・変異検証

- 2026-09-18: issue 394 (VFR → CFR 正規化) は done。本 issue は継続 (pending のまま)
