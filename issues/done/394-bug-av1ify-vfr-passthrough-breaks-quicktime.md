# av1ify: VFR ソースのタイムスタンプをそのまま通すため、出力が QuickTime で再生破綻する (VLC は無症状)

起票日: 2026-09-18
種別: bug
優先度: P2 (再生は VLC 等の寛容なプレイヤーでは無症状。QuickTime/AVFoundation 系プレイヤーでのみ症状が出る)

## 何が起きるか

`av1ify` で出力した MP4 (映像 `libsvtav1`) を QuickTime Player で再生すると、
約5秒おき(映像のキーフレーム間隔と一致)にブロックノイズが画面を覆う。
同じファイルを VLC で再生すると問題なし。

## 原因

`av1ify` はユーザーが `-fps` オプションを明示しない限り、`ffmpeg` に `-r` / `-fps_mode` を
一切渡さない (`zshlib/_av1ify_encode.zsh` の `args_common` 構築部)。ソースが可変フレームレート
(VFR) の場合、ソースのタイムスタンプがそのまま SVT-AV1 エンコードへ渡り、**出力側の映像ストリームの
DTS が平均 1 秒に1回、直前フレームと同値(非単調増加)になる**。

`-fflags +genpts` で再生成しても変化しない(=「欠落」ではなく「そう刻まれた」タイムスタンプ)ため、
container レベルの remux では直せない。QuickTime (AVFoundation) はコンテナの DTS を厳密に信頼して
B-フレームの参照・並べ替えを行うため、DTS衝突のたびに描画が乱れる。VLC (dav1d) はより寛容に
扱えるため症状が出ない。

## 再現 (実ファイルで確認)

対象: ユーザーのローカル環境にある動画ファイル (元ソース。`r_frame_rate=29/1` だが
`avg_frame_rate=29970029/1000000` と乖離しており VFR、尺は約2時間21分)。これを `av1ify` で
エンコードした出力 (映像 `libsvtav1`, 640x480) で確認 (ファイル名はプライバシーのため省略)。

```sh
# 映像ビットストリーム自体は健全 (dav1d フルデコードでピクセルレベルの警告 0件)
ffmpeg -y -v warning -i "<encoded.mp4>" -f null - 2>decode_err.log
# → decode_err.log は「non monotonically increasing dts」のみ 8230件 (8494秒中、平均1.03秒に1回)
grep -v "non monotonically increasing dts" decode_err.log | wc -l   # → 0 (他の警告・エラーは無い)

# +genpts で remux しても症状は変化しない (同じ8230件が同じ値で再現)
ffmpeg -y -fflags +genpts -i "<encoded.mp4>" -c copy -movflags +faststart out.mp4
ffmpeg -y -v warning -i out.mp4 -f null - 2>verify_err.log
wc -l verify_err.log   # → 8230 (変化なし。genpts はここでは無力)
```

キーフレーム間隔 (`ffprobe -show_entries frame=key_frame,pts_time`) は約 5.4 秒おきで、
ユーザー報告の「5秒おきのブロックノイズ」と一致する。

## 直したい方針 (根本原因の是正、症状対処ではない)

`args_common` に常に `-fps_mode cfr` を追加し、VFR ソースであっても常に等間隔・単調増加の
タイムスタンプでエンコードする。`target_fps` が明示指定されている場合はそちらが優先されるので
競合しない (`-r` と `-fps_mode cfr` は併用可能)。

案:

```zsh
args_common=(
  -hide_banner -nostdin -stats -y
  -i "$in"
  -map "0:v:0"
  -fps_mode cfr
  -c:v "$vcodec" -crf "$crf" -preset "$preset" -pix_fmt yuv420p
)
```

## Todolist

- [x] `_av1ify_encode.zsh` に `-fps_mode cfr` を追加 (常時付与)
- [x] 既存テスト (`tests/zshrc/av1ify/*.sh`, `tests/zshrc/validate-mp4/*.sh`) が通ることを確認
- [x] 新規テスト: VFR 検出 (mock ffprobe の `MOCK_FPS`/`MOCK_AVG_FPS`) で `-fps_mode cfr` +
      明示 `-r <avg fps>` が ffmpeg へ渡ることを argv ログで assert (`test_av1ify_fps_mode.sh`)
- [x] 追加したテストに変異検証 (3 箇所、いずれも red 確認)
- [x] codex review → **codex が利用上限で不可 (2026-09-18, "usage limit" エラー)**。代替として
      観点を分けた read-only サブエージェント 3 体 (①壊す ②素通り ③回帰) の反証レビューを実施
- [x] 修正版 av1ify で上記のローカル再現ファイルを再エンコードし、DTS 単調増加を確認

## レビューで判明した追加対応 (当初案からの変更点)

反証レビュー (③回帰 P1) で「`-fps_mode cfr` を無条件追加すると、`__av1ify_postcheck` の
フレーム数不一致チェック (`fps_changed=0` のときのみ有効) が VFR ソースに対して誤 NG を
出しうる」との指摘。実際、再現ファイルの DTS 衝突頻度 (平均1.03秒に1回) から見積もると、
デフォルトの相対許容 (0.5%) を優に超える規模のフレーム数差が起こりうる。

対応: `__av1ify_detect_vfr` を新設し、`r_frame_rate` と `avg_frame_rate` の相対差が
`AV1IFY_FRAME_TOLERANCE_PCT` (既定0.5%) を超えるソースを VFR と判定。VFR 検出時は
①明示 `-r <avg_frame_rate>` を渡す (②の指摘: ffmpeg が cfr 変換の基準に r_frame_rate と
avg_frame_rate のどちらを使うか曖昧なため明示化) ②postcheck へ `fps_changed=1` 相当を
伝えてフレーム数不一致チェックを抑制する (CFR ソースでは検出されないため、そちらの
チェックは従来どおり有効)。

また (②素通り P2) の指摘で、`_validate_mp4.zsh` の `__VALIDATE_MP4_DECODE_ERROR_RE` に
"non monotonically increasing dts" を追加。これは av1ify 出力の post-encode 全デコード検証
(`__validate_mp4_check`) が既に同じ `ffmpeg -f null -` を実行しているため、将来
`-fps_mode cfr` の配線が外れたときの安全網として機能する (`test_validate_mp4.sh` に
mutation-verified な回帰テストを追加)。

## 進捗

- 「av1ify: VFR ソースを常時 CFR へ正規化し QuickTime での再生破綻を防ぐ (issue 394)」:
  実装・テスト・反証レビュー反映 (上記)

## 結果 (実測 2026-09-18)

修正版 av1ify で元ソースを再エンコード (av1ify 自身の validate 通過、`check_ng` なし):

| 項目 | 修正前の出力 | 修正後の出力 |
|---|---|---|
| `non monotonically increasing dts` (全編デコード `-v warning`) | 8230 件 | **0 件** |
| 警告総数 | 8230 件 | **0 件** |
| r_frame_rate / avg_frame_rate | 29/1 / 7374294/246329 | 2997/100 / 2997/100 |
| duration | 8494.10s | 8494.09s |

ffmpeg の進捗表示で `dup=114 drop=0` (CFR 正規化で複製されたフレーム数)。

速度 (同一 60 秒区間、libsvtav1 crf40 preset5、映像のみ、交互に 4 回。本番エンコードと並行実行):
`-fps_mode cfr -r 29.970` あり 4.23〜4.32x / なし 1.24〜1.35x。修正による速度低下は無い
(ありの方が速い理由は未調査)。

## 残タスク

- スコープ外: 他の破損パターン (ASF 末尾破損 / WMV3 散在破損 + 音声 DTS 異常) は issue 395 (pending)
- 未検証: QuickTime Player での目視確認 → issue 396 (human) へ切り出し
