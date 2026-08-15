# av1ify: 開始シフト + 音声を長めに残す形が、avsync 初回ゲートを潜って検知されない

起票日: 2026-08-15
種別: bug
優先度: **P2** (実害級 (〜4s) の音ズレを ✅ 完了で通しうる。ただし av1ify 自身の encode が
この形を作る実証は無く、postcheck という網の識別能力の話。058 の敵対的レビューで発見)

## 何が起きるか

`__av1ify_postcheck` の avsync 判定は 2 段構え:

1. **初回**: 宣言 duration ベースの A/V drift が閾値 (既定 2.0s) を超えたときだけ FLAG
2. **再判定**: FLAG が立ったときだけ packet 実測 + 開始オフセットで降格を判断 (issue 058 で修正)

問題は **1 の初回ゲートを潜る形**。音声を後ろへ N 秒シフトし、音声の**長さ**を映像に近づける
(宣言 duration drift < 2.0 に収める) と、初回判定が FLAG せず、058 で足した開始シフト
チェック (`__av1ify_start_time`) に**構造的に到達しない**。開始オフセット N は最大 ~2×閾値
(≈4s) まで取れる。

## 再現 (実 ffmpeg 8.0.1 / ffprobe で実測)

```sh
# src: 30s testsrc + sine (h264/aac)
ffmpeg -y -f lavfi -i "testsrc=size=320x240:rate=30:duration=30" \
       -f lavfi -i "sine=frequency=440:duration=30" \
       -c:v libx264 -preset ultrafast -c:a aac -shortest src.mp4
# 音声を 3.8s 後ろへ + 28.1s ぶん残す (-t で切らない = 音声終端は映像より長い)
ffmpeg -y -i src.mp4 -itsoffset 3.8 -i src.mp4 -map 0:v -map 1:a \
       -af "atrim=duration=28.1" -c:v libx264 -preset ultrafast -c:a aac outG-enc.mp4
```

ffprobe 実測:

```
outG  0,video,0.000000,30.000000
      1,audio,3.776009,28.123220   ← 開始 3.78s シフト
format=duration 31.899229
```

`__av1ify_postcheck outG-enc.mp4 src.mp4` の出力に **avsync サフィックスが付かない**
(実測は h264 ハーネスのため「映像コーデック不一致」だけで rc=1 だが、本物の AV1 出力なら
avsync でもコーデックでも拾えず **rc=0 = ✅ 完了**)。

理由:
- 宣言 drift = |28.12 − 30| = **1.88 < 2.0** → 初回 FLAG せず、開始シフトチェックに未到達
- format duration の再生時間比較も Δ=1.9s で閾値内

3.78s の全編音ズレはリップシンク体感として明確に実害級 (2s 閾値の設計意図を大きく超える)。

## 058 との関係 (これはデグレではない)

058 の修正が守るのは「FLAG が立った後の降格の正しさ」だけで、そこは 058 の敵対的レビュー
(観点 B/C) が「壊せなかった」と実測確認済み。本件は初回ゲートを潜る**別の穴**で、058 の
修正コメントも「宣言ベースの初回判定を素通りするズレは変更前 (997d078) も検知しておらず、
ここで守るのは降格の正しさだけ」と明示的にスコープ外と宣言している。997d078 でも同じく
素通りする (初回 FLAG しないため再判定に入らない)。

## 対応方針 (案)

058 の敵対的レビュー (観点 A) の提案:

- **開始オフセット (start_time) の関係差チェックを、初回ゲートの内側でなく常時 (宣言 drift の
  大小に依らず) 走らせる**。`__av1ify_start_time` は ffprobe 1 フィールド × 4 の安価な検査で、
  058 が「双方向化 (ok→FLAG) を避けた」理由 (packet 走査 4 本の常時コスト) には該当しない。
  start_time だけなら常時化してもコスト増はほぼ無い
- ⚠️ 常時化すると誤検知 (false positive) の回帰リスクが戻るので、058 の観点 C レビュー
  (AAC priming は実測 20〜50ms で閾値 2.0s の 2 桁下 / TS 等の全体ベースシフトは関係差で相殺)
  の結果を踏まえ、**関係差 (a_start − v_start の src/out 差分) で見る**こと。絶対 start では
  正常な TS 抜き素材で誤検知する

## 未確認

- **av1ify 自身の encode がこの形 (開始シフト + 音声を映像より長く残す) を作るか**は未実証。
  再現は `-itsoffset` の人工合成。postcheck は想定外の ffmpeg 挙動を拾う網なので、網の穴
  としては成立する (058 と同じスタンス)
- 常時 start チェックを入れたときの、壊れた宣言 duration ソース (058 の Test 14 が守る誤検知
  是正ケース) との相互作用 — start が揃っていれば降格されるはずだが要実測

## 関連

- `issues/done/058-bug-av1ify-avsync-false-negative.md` (本件の発見元。058 の修正自体は正しい)
- `_claude/rules/adversarial-review-own-safeguards.md` — 「検査・ゲートは検査できなかったときに
  緑を返さない」。本件は「初回ゲートが閉じているときに検査自体が走らない」形
