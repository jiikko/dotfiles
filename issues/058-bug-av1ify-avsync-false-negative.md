# av1ify: avsync の誤検知是正が「時間シフト型の音ズレ」を素通りさせるようになった (検知漏れ)

起票日: 2026-08-14
種別: bug
優先度: **P1** (本物の音ズレを ✅ 完了として通す。`--delete-origin` 併用時は元ファイルが trash へ回る)

## 何が起きるか

`3640291` (avsync 誤検知の是正) 以降、**音声トラックが丸ごと時間シフトした出力**を
`__av1ify_postcheck` が正常と判定するようになった。

変更前は `音ズレ疑い` を出して `-check_ng-avsync-` へリネームし NG を返していた。
変更後は「packet 実測では正常」と表示して警告を消す。

⚠️ `av1ify --delete-origin` を使っている場合、**元ファイルが trash
(ネットワーク FS では `rm`) へ回る**。既定は `__AV1IFY_DELETE_ORIGIN=0` なので
オプション指定時のみだが、音ズレした出力だけが残る形になりうる。

## 再現 (独立に 2 回確認済み)

```sh
mkdir -p ./tmp/rv-avs && cd ./tmp/rv-avs
ffmpeg -y -loglevel error -f lavfi -i "testsrc=size=320x240:rate=30:duration=20" \
       -f lavfi -i "sine=frequency=440:duration=20" \
       -c:v libx264 -preset ultrafast -c:a aac -shortest src.mp4
# 音声だけ 5s 後ろへずらし、映像と同じ 20s で切る (= 実害のある音ズレ)
ffmpeg -y -loglevel error -i src.mp4 -itsoffset 5 -i src.mp4 \
       -map 0:v -map 1:a -c copy -t 20 out-enc.mp4

zsh -c "source <各版の zshlib/_av1ify_postcheck.zsh>; __av1ify_postcheck \$PWD/out-enc.mp4 \$PWD/src.mp4"
```

⚠️ postcheck は NG 時にファイルをリネームするので、**版ごとに fixture を作り直す**こと
(使い回すと 2 回目が「音声ストリーム検出できず」になる)。

### ffprobe の宣言値 (ffprobe 8.0.1)

```
-- src.mp4      (index,codec_type,start_time,duration)
0,video,0.000000,20.000000
1,audio,0.000000,20.000000
-- out-enc.mp4
0,video,0.000000,20.000000
1,audio,4.976009,15.023311     ← 音声が 4.976s 後ろへシフト
```

### 結果

| `_av1ify_postcheck.zsh` の版 | 出力 | リネーム後 |
|---|---|---|
| `997d078` (変更前) | `⚠️ チェック警告: 音ズレ疑い (src_delta=0.000000s out_delta=-4.976689s Δ=4.976689s threshold=2.0s), 映像コーデック不一致` | `out-check_ng-`**`avsync`**`-codec-enc.mp4` |
| **HEAD** | `>> 音ズレ判定: 宣言 duration ベースでは Δ=4.976689s だが packet 実測では Δ=0.000680s のため正常と判定 (ソースの宣言 duration が不正確)`<br>`⚠️ チェック警告: 映像コーデック不一致` | `out-check_ng-codec-enc.mp4` (**avsync が消えた**) |

コーデック不一致はハーネスが h264 を使っているためのノイズで、avsync 判定とは独立。

### 本物の AV1 で再検証 (コーデックのノイズなし)

出力を実際に AV1 でエンコードして同じ比較を行った結果:

```
-- 宣言値 (out2-enc.mp4)
0,av1,video,0.000000,20.000000
1,aac,audio,4.976009,15.023311
-- 表示終端 (packet 実測 max(pts+duration))
src.mp4       v:0 -> 20.000000 / a:0 -> 20.000000
out2-enc.mp4  v:0 -> 20.000000 / a:0 -> 19.999320

=== 997d078 (変更前) ===
⚠️ チェック警告: 音ズレ疑い (Δ=4.976689s threshold=2.0s)
[rc=1]                        → p2-check_ng-avsync-enc.mp4 へリネーム

=== HEAD ===
>> 音ズレ判定: ... packet 実測では Δ=0.000680s のため正常と判定
[rc=0]                        → p2-enc.mp4 のまま素通り
```

**`rc=0` = 全チェック通過 = `✅ 完了`。リネームもされない。**
実害の経路は `zshlib/_av1ify_encode.zsh:94` (postcheck が 0 → `✅ 完了` を表示) と
同 `:109` (`__AV1IFY_DELETE_ORIGIN` が立っていれば元ファイルを trash / ネットワーク FS では `rm`)。

## 原因: 「長さ」と「表示終端」は別の量

再判定は 4 値すべてを `__av1ify_packet_end` = `max(pts_time + duration_time)` で測り直す。

- `stream=duration` … ストリームの**長さ**
- `__av1ify_packet_end` … **開始オフセットを含む表示終端**

音声を後ろへずらして映像と同じ終端で切った場合、**表示終端は映像と揃ったまま**なので
Δ≈0 になる。長さで測れば 15.02 vs 20.00 で差が出るが、終端で測ると差が消える。

コミットメッセージの

> false negative は増えない: 本物の encode 起因ズレは出力の packet 列に必ず出る

は、この量の違いを見落とした誤り。**時間シフト型のズレは packet 列の「終端」には出ない**
(出るのは「開始」と「長さ」)。

## 構造的な問題: 再判定が一方向にしか働かない

`zshlib/_av1ify_postcheck.zsh:221-245`:

```sh
if [[ "$drift_bad" == "1" ]]; then      # ← 既に FLAG が立っているときしか走らない
    ...
    sd_v="$m_sd"; od_v="$m_od"; drift_v="$m_drift"; drift_bad="$m_bad"   # ← 上書き
fi
```

- 再判定は `drift_bad == 1` の内側にしか無い
- 効果は `drift_bad` を上書きすることだけ

→ **FLAG→ok の一方向にしか働かない。ok→FLAG は構造的に起こり得ない。**
誤検知を消す方向にだけ効くゲートなので、検知漏れを増やす方向にバイアスがかかっている。

なお `__av1ify_packet_end` の doc は

> 過小評価は drift を縮める方向に働き、本物の音ズレを見逃す。

と**この失敗方向を明示的に警告している** (packet 列の最終行を終端に使う罠について)。
同じ危険が「長さ→表示終端」のすり替えにもあることは書かれていない。

## 検知漏れするクラスの厳密な定義 (敵対的検証で発火条件を絞り込んだ)

再判定は**初回 (宣言 duration ベース) が閾値を超えたときにしか走らない**ため、
`start_time > 0` かつ「表示終端が揃っている」だけでは足りない。
**音声の宣言 length が閾値 (既定 2.0s) を超えて縮んでいる**ことも必要。

> **検知漏れするのは「音声の先頭 N 秒 (N > 2.0) が落ちて edit-list 遅延が書かれ、
> 表示終端は映像と揃っている出力」**

逆に「音声の長さは保ったまま丸ごと後ろへずらした」形 (出力 25s / 音声 20s @ start 5s) は
初回 `od=0` で再判定に到達せず、**997d078 でも avsync では検知していなかった**
(この形は format duration 差で「再生時間ズレ」が別途拾う)。
したがってデグレの範囲は **「シフト + 映像終端での切り詰め」型に限られる**。

## 既存テストがこのクラスを守れない理由

`tests/zshrc/av1ify/test_av1ify_avsync.sh` の Test 15
「genuine drift survives packet re-measurement」は `MOCK_OUTPUT_AUDIO_LAST_PTS=103.0` を渡しており、
**packet 実測側にも drift が出る形しか作っていない**。

根本は mock の表現力: `tests/zshrc/av1ify/test_helper.sh:124-130` の mock ffprobe は
**1 ストリームにつき単一の LAST_PTS しか返さず `start_time` の概念を持たない**ため、
「終端は揃うが先頭がずれる」形を mock では表現できない。
著者の変異検証が通ったのはこのため。

## 対応方針 (案)

1. **再判定を「終端」だけでなく「開始 + 長さ」で行う**。`start_time` の差 (src と out で
   音声の開始オフセットが変わっていないか) を独立に見る。時間シフトはここに必ず出る
2. **再判定を双方向にする**。`drift_bad == 0` でも packet 実測でズレが出たら FLAG を立てる。
   今の実装は「誤検知を消す」専用のゲートで、`adversarial-review-own-safeguards.md` の
   「検査できなかったときに緑を返さない」に反する形になっている
3. **元の誤検知 (壊れた宣言 duration) と時間シフトを区別する条件を明示する**。
   誤検知の実例は「宣言 duration がサンプルテーブルと食い違う」ケースなので、
   `start_time` が 0 のまま長さだけズレている場合に限定すれば、シフト型は救われない
4. **mock ffprobe に `start_time` を持たせる**。これが無いと本クラスの回帰テストが
   そもそも書けない (上記「既存テストが守れない理由」)。その上で
   `tests/zshrc/av1ify/test_av1ify_avsync.sh` に「終端は揃うが先頭がずれる」ケースを足し、
   **NG になること**を assert する。3640291 が足したテストは誤検知側
   (壊れた宣言 duration → OK になる) だけを見ている

## 未確認

- **本再現は `-itsoffset` で出力の形を人工的に合成したもの**で、av1ify 自身の encode が
  この形を作ることは実証していない。実証できたのは
  「**postcheck という安全機構の識別能力が落ちた**」= 997d078 が NG にしていた実害級の出力を
  HEAD が `✅ 完了` で通すこと。postcheck は想定外の ffmpeg 挙動を拾うための網なので、
  この範囲でもデグレとして成立する (エンコーダのバグの証明ではない)
- `__av1ify_packet_end` が「末尾 60s 区間だけ走査する」ことによる、
  60s 以上のシフトでの挙動は未検証
- 音声が**前に**シフトしたケース (start_time < 0 / edit list による負のオフセット) は未検証

## 関連

- `_claude/rules/adversarial-review-own-safeguards.md` — 「検査・ゲートは検査できなかったときに
  緑を返さない」。本件は「誤検知を消す方向にだけ効くゲート」で同じ穴
- `~/.claude/CLAUDE.md`「不具合対応の原則」— 誤検知の是正が検知漏れを生む典型。
  「この if 文を足せば直る」で入れた再判定が、前提 (測る量) の是正になっていない
- issue 057 — 同じ夜の「片方向だけ直して鏡像のバグを作った」形 (あちらはトースト)
