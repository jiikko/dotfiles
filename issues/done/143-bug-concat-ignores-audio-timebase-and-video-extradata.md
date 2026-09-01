# 143 bug: concat が音声 time_base と映像 extradata を見ておらず、壊れた結合を成功扱いする

> ⚠️ **2026-08-30 に 139 → 143 へ改番**。同じ 139 が 2 件あった
> (`issues/done/139-bug-test-runner-cannot-distinguish-skip-from-pass.md`)。あちらは tracked 2 ファイル
> (`Makefile` / `tests/CLAUDE.md`) と push 済み commit の**件名** (ec12ee6 `... (issue 139)`) から
> 参照されており、commit message は履歴なので直せない。こちらの参照は tracked 2 行
> (`zshlib/_concat_helpers.zsh`) と commit 94a4b3c の**本文**だけだったので、こちらを動かした。
> **過去の会話やメモの「139」が concat の話なら、それはこの issue。**

- カテゴリ: bug
- 起票: 2026-08-30
- 状態: done (2026-09-02)

## 要約

`concat` の「再エンコード回避チェック」は映像の `codec_name,width,height,pix_fmt` と
**映像の** `time_base` しか見ていない。次の 2 つが素通りし、**壊れた出力を exit 0 で
成功扱いして元ファイルをゴミ箱へ送る**。

1. **音声 time_base の不一致** — 音声が倍近くまで伸びる
2. **映像 extradata (SPS/PPS/hvcC) の不一致** — 後半セグメントがデコードエラーになる

どちらも合成素材で再現済み（下記）。**フレームレート不一致の警告化 (2026-08-30) とは
独立した既存欠陥**で、フレームレートを揃えた条件でも同じように発火することを実験で
確認している（＝あの緩和が作った穴ではない）。

## 再現 1: 音声 time_base の不一致

映像側は codec / 解像度 / pix_fmt / time_base をすべて揃え、音声も codec / sample_rate /
channels を揃える。違うのは音声の time_base だけ（mp4 は 1/44100、MPEG-TS は 1/90000）。

```sh
ffmpeg -f lavfi -i testsrc=size=320x240:rate=30:duration=2 \
  -f lavfi -i sine=frequency=440:sample_rate=44100:duration=2 \
  -c:v libx264 -pix_fmt yuv420p -c:a aac -ar 44100 -ac 1 \
  -video_track_timescale 90000 -y same_001.mp4
ffmpeg -f lavfi -i testsrc=size=320x240:rate=30:duration=2 \
  -f lavfi -i sine=frequency=440:sample_rate=44100:duration=2 \
  -c:v libx264 -pix_fmt yuv420p -c:a aac -ar 44100 -ac 1 -y same_002.ts

concat --keep same_001.mp4 same_002.ts
```

実測（2026-08-30）:

| | 期待 | 実際 |
|---|---|---|
| 映像 duration | 4.0s | 4.023222s |
| 音声 duration | 4.0s | **8.274966s** |

`concat` は警告を 1 つも出さず ✅ 完了。合計 10 秒以下なので duration 診断も働かず、
フレーム順序検証は映像しか見ないため通過する。`--keep` を外せば元ファイルはゴミ箱へ行く。

## 再現 2: 映像 extradata の不一致

HEVC の CTU サイズだけを変え、codec / 解像度 / pix_fmt / time_base / フレームレートを
すべて揃える。

```sh
ffmpeg -f lavfi -i testsrc=size=320x240:rate=30:duration=2 \
  -c:v libx265 -x265-params 'ctu=64:log-level=none' -pix_fmt yuv420p \
  -video_track_timescale 90000 -an -y scene_001_take.mp4
ffmpeg -f lavfi -i testsrc=size=320x240:rate=30:duration=2 \
  -c:v libx265 -x265-params 'ctu=16:log-level=none' -pix_fmt yuv420p \
  -video_track_timescale 90000 -an -y scene_002_take.mp4

concat --keep scene_001_take.mp4 scene_002_take.mp4
ffmpeg -v error -i scene.mp4 -f null -
```

実測（2026-08-30）: `concat` は ✅ 完了。出力をデコードすると
`[hevc] The cu_qp_delta 27 is outside the valid range [-26, 25].` が出る
（`extradata_size` は 2432 / 2430、`extradata_hash` は `CRC32:6ae853fb` / `CRC32:9e938c80`）。

なお `_NNN_take` という名前はフレーム順序検証の連番抽出に引っかからず、検証自体が
skip される（`__concat_verify_frame_order` は連番を取れないと成功扱いで return 0）。
ただしこの skip を直してもこの破損は捕まらない（「却下した指摘」参照）。

## 対応案

### 音声 time_base — `__concat_get_audio_info` に `time_base` を足す

既存の「音声情報不一致」エラー経路にそのまま乗る。mp4 の AAC トラックは timescale が
sample_rate と一致するのが通例なので、同じ容器どうしでは発火しない（誤検出しにくい）。

**修復は再エンコード不要**。MPEG-TS 側を `ffmpeg -i in.ts -c copy -y out.mp4` と remux
するだけで音声 time_base が 1/90000 → 1/44100 に正規化されることを実測した
（2026-08-30）。エラーメッセージでは remux を案内すればよい。
`repair-mp4-timebase` が映像専用（`-video_track_timescale`）なのは現ツールの制約であって、
音声を再エンコードしなければ直らないという意味ではない。

### 映像 extradata — `extradata_hash` の単純比較は**使えない**

当初「`__concat_get_video_info` に `extradata_hash` を足すだけ」と書いていたが、
2026-08-30 の codex 反証レビューと自前の実測で **誤りと確認したので撤回する**。

raw extradata のハッシュは「繋げるかどうか」と一致しない:

| 条件 | extradata_hash | 実際に繋げるか |
|---|---|---|
| 同一エンコーダ・同一設定・同一 fps・同一容器 | 一致 (h264 / AV1 で確認) | ○ |
| **fps だけ違う** (libx264 30fps / 60fps) | **不一致** (`6fa5c24b` / `0f301095`) | ○ 繋げる |
| **容器だけ違う** (mp4 の avcC / MPEG-TS の Annex B) | **不一致** (`6fa5c24b` / `ef5b433d`) | ○ 繋げる（concat demuxer が `h264_mp4toannexb` を自動挿入する） |
| x264 preset 違い (medium / ultrafast) | 不一致 | ○ 全フレームのデコードが通った |
| HEVC の CTU サイズ違い | 不一致 | ✗ デコードエラー |

fps 差で hash が変わるのは SPS にタイミング情報が入るため。これを映像情報比較に足すと、
**フレームレート不一致が警告処理に到達する前に「映像情報不一致」で落ちる** —
2026-08-30 のフレームレート緩和を事実上取り消してしまう。

有望なのは属性の突き合わせではなく **出力側の検査**。結合後の出力をデコードして
エラーが出ないことを見れば、extradata 不一致も未知の破損もまとめて捕まる:

```sh
ffmpeg -v error -i "$output" -f null -   # stderr が空でなければ破損
```

全長デコードは長尺で重いので、セグメント境界の前後だけを対象にする形が現実的。
既存の `__concat_verify_frame_order` は境界付近のフレームハッシュを取っているので、
そこにデコード時の stderr 検査を足すのが最短だと思われる（未検証）。

### どちらにも共通する前提 — 「検査できなかった」を緑にしない

現在の helper は `ffprobe ... 2>/dev/null | head -n1` で、次を区別できない:

- ffprobe 自体の失敗
- そのフィールドに非対応な ffprobe
- 正常だが値が存在しない (extradata を持たない stream 等)

全入力で空になれば比較は一致して素通りし、逆に空を一律エラーにすると正常なケースを
拒否する。フィールドを 1 つ足すだけでは fail-closed にならないので、helper 側で
「取得できなかった」を値の不一致とも一致とも違う第 3 の結果として扱う必要がある。

## 経緯

2026-08-30、フレームレート不一致チェックをエラーから警告へ緩めた変更
（`zshlib/_concat.zsh`）の敵対的レビューで codex が指摘した。指摘は
「フレームレート差が偶然この 2 つをマスクしていた」という形だったが、実験で
フレームレートを揃えても同じように発火することを確認したため、**緩和とは独立の
既存欠陥**として切り出した（この切り分けは codex の再レビューでも反証されなかった）。

同じ再レビューで、当初の対応案「extradata_hash を足すだけ」が誤りであることが判明し、
上記のとおり撤回・書き換えた。

## 却下した指摘

- **「フレーム順序検証が `_NNN_take` の連番を抽出できず skip される点を直せば捕まる」**
  — 捕まらない。連番を認識するようにしても、後半のデコードに失敗すると
  `__concat_frame_hash` が空を返し、`__concat_verify_frame_order` は警告を出すだけで
  成功扱いのまま続行する。連番正規表現の修正は extradata 不一致の検出策にならない
  （skip 経路そのものの是非は別の話として残る）。

## 決着 (2026-09-02)

3 点とも実装した。実 ffmpeg 8.0.1 で本 issue の再現 1 / 2 を流し、どちらも拒否されて出力が残らないこと、
同一ファイルの `-c copy` コピー同士 (clip_001/002) は境界デコード検証を通って ✅ になることを確認した。

- **音声 time_base**: `__concat_get_audio_time_base` を足し、`_concat.zsh` の映像 time_base チェックの直後に
  独立チェックを置いた (`音声情報不一致` = 再エンコード案内に混ぜない)。基準は `1/<sample_rate>` で、外れている
  側に `ffmpeg -i in -c copy out_remux.mp4` を案内する。実測: same_001.mp4 (1/44100) + same_002.ts (1/90000)
  → same_002.ts だけを remux 対象として案内
- **映像 extradata**: `__concat_verify_decode` を足し、フレーム順序検証の後に各セグメント境界の前後 1 秒を
  `ffmpeg -v error ... -f null -` でデコードする。stderr 非空か exit 非 0 で失敗 → 出力を消して元ファイルを残す。
  実測: scene_001_take/scene_002_take (CTU 64/16) → 境界 2.000s で `cu_qp_delta 27 is outside the valid range` を
  捕まえて拒否 (連番名のときはフレーム順序検証が先に落とす)
- **検査できなかったを緑にしない**: `__concat_get_video_info` / `__concat_get_audio_info` /
  `__concat_get_audio_time_base` は `| head -n1` をやめて ffprobe の exit code を返す。失敗は
  「ffprobe 失敗」として拒否 (旧実装は先頭ファイルで失敗すると音声チェックごと skip、2 本目以降で失敗すると
  「再エンコードが必要」と誤案内していた)。テストの mock はこれを模して `probefail_002` で exit 1 を返す
- テスト: `tests/zshrc/concat/test_concat_audio_time_base.sh` / `test_concat_decode_check.sh`。変異検証:
  不一致判定を潰す → red / デコード判定を潰す → red / audio_info の `|| return 1` を外す → red
- 敵対的レビューは通していない (codex は自発起動しない運用)。ffprobe の実 CLI 契約は本セッションで
  stdout / exit code を分けて実測した (音声なし → 空・rc 0 / ファイルなし → rc 1)
