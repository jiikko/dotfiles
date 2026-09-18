# av1ify: VFR 正規化の `-r` が誤った値になる経路が 4 本あり、それを検出する唯一の検査を同じ変更が切っている

起票日: 2026-09-19
カテゴリ: bug / priority: **high**
対象: `zshlib/_av1ify_encode.zsh` の `__av1ify_frac_fps` / `__av1ify_detect_vfr` / 引数組み立て (`elif (( __AV1IFY_R_VFR_DETECTED ))`) / `__av1ify_finalize` の `_fps_changed`、`zshlib/_av1ify_postcheck.zsh` のフレーム数チェック、`tests/zshrc/av1ify/test_av1ify_vfr_e2e.sh`
出典: [issue 394](done/394-bug-av1ify-vfr-passthrough-breaks-quicktime.md) の実装 (3 commit) への敵対的レビュー (観点 3 分割: 壊す / 素通り / 回帰)
反証レビュー: 3 観点で実施。**下記の数値はすべてレビュー時の実測** (ffmpeg 8.0.1 / SVT-AV1 4.0.1、使い捨て worktree)

## 問題

issue 394 の実装は「VFR 検出時だけ `-fps_mode cfr -r <avg fps>` を付ける」。この `-r` に**誤った値**が
渡る経路が 4 本あり、かつ**その誤りを検出できる唯一の production 検査を同じ変更が無効化している**。
結果として「映像の大半が失われた出力を正常と判定し、元ファイルを削除する」経路が開いている。

### 1. `-r` を誤らせる 4 経路

| # | 経路 | 症状 |
|---|---|---|
| a | 新規 awk に `LC_ALL=C` が無い | 小数点がカンマのロケール (de_DE 等) で `-r "29,970"` → `Invalid framerate value` でエンコード失敗 |
| b | `__av1ify_frac_fps` の分母 0 フォールバック | `24/0` (不明) が `24.000` に**捏造**される (`0/0` はガードに掛かるが `24/0` は通る) |
| c | `--fps N` のキャップ判定が `r_frame_rate` 基準 | `r <= N < avg` の VFR ソースで `target_fps` が空になり VFR 分岐が発火、**指定より高い avg fps** で出力される |
| d | `-r` が `%.3f` 丸めの 10 進 | 相対誤差が fps に反比例。`duration > 4000 × avg` で duration が 2 秒以上ズレる (avg=0.1fps なら 7 分弱で発火) |

a は同 repo の `_av1ify_postcheck.zsh` が 121/124/133/162 行で**全て `LC_ALL=C awk`** を使っている
のに対し、新規コードだけが規律から外れた形。今回**初めてこの文字列が外部コマンドの引数になった**
ことで実害が出るようになった (`__av1ify_decide_fps` の既存 awk は比較にしか使わないので無害だった)。

### 2. 誤りを検出する唯一の検査を、同じ変更が切っている

`__av1ify_finalize` の `_fps_changed=1` が抑制する `__av1ify_postcheck` のフレーム数チェックは、
**パイプライン唯一の「フレーム密度」検査**。他の映像検査は全てエンドポイント (span) 検査なので
代替にならない。

| 検査 | 測るもの | 密度の誤りを拾えるか |
|---|---|---|
| 再生時間ズレ (`AV1IFY_DURATION_TOLERANCE` 2.0s) | `format=duration` の差 | ✗ (下表) |
| 映像尺不一致 vidloss | 映像ストリームの終端 | ✗ |
| 音ズレ avsync | A/V ギャップの変化 | ✗ |
| **フレーム数** | **フレーム密度 (件数)** | ○ ← これを切っている |

**実測** (src = 30fps / 10s / 300 フレームを `-fps_mode cfr -r X` で作り直したとき):

| `-r` | 出力フレーム | 欠落率 | duration Δ | 2.0s 閾値 |
|---|---|---|---|---|
| 24 | 242 | 19% | 0.083s | 通過 |
| 15 | 152 | 49% | 0.133s | 通過 |
| 5 | 52 | 83% | 0.400s | 通過 |
| **1** | **12** | **96%** | **2.000s** | **通過** (判定は `> 2.0` なので不合格にならない) |

**96% のフレームを失っても duration チェックは 1 度も発火しない。** しかもこの Δ は尺に比例しない —
120 秒 / 30fps (3600 フレーム) を `-r 1` にしても **3600 → 122 フレーム、Δ = ちょうど 2.000s** で通過した。
CFR retiming は「同じタイムラインを別の密度で埋める」操作なので、**duration の変位はターゲット fps の
1〜2 フレーム間隔が上限** (= 尺に無関係)。duration チェックはこの帯を**原理的に**拾えない。

本物の `__av1ify_postcheck` を通した A-B (src=300 フレーム、out=12 フレーム):

```
fps_changed=0 → 🚨 フレーム数不一致 (src=300, out=12, Δ=288, 許容=24)  → check_ng
fps_changed=1 → (frames の指摘が消滅。他の検査は 1 つも発火しない)     → OK
```

本番で OK は `__av1ify_finalize` が**元ファイルをゴミ箱へ移す**ことを意味する。

### 3. テストもこの誤りを見ていない

`tests/zshrc/av1ify/test_av1ify_vfr_e2e.sh:82` の CFR 判定は

```sh
check_eq "$(v_field "$out" avg_frame_rate)" "$(v_field "$out" r_frame_rate)" "出力の avg == r (CFR)"
```

で、**出力どうしの自己比較**。「CFR か」しか見ておらず「**何 fps の CFR か**」を見ていない。
変異 `-r "$__AV1IFY_R_VFR_AVG_FPS"` → `-r 10` を当てると **e2e は 7/7 で完全に緑**のまま、
ソース 154 フレーム中 92 フレームが drop され (`dup=0 drop=92`)、av1ify は
`✅ 完了 📉 216.9 KB → 66.1 KB (-70%)` を出して元ファイルを削除した。

`-r` を丸ごと落とす変異でも e2e は緑 (`test_av1ify_fps_mode.sh` だけが red)。つまり
「ffmpeg がどちらを基準にするか曖昧なので `-r` を明示する」というコード側の主張は**実測で
裏付けられていない** (fixture は `-r` 無しでも CFR になった)。

## なぜ single の bug でなく「構造」なのか

a〜d は単独ではどれも軽微 (ロケール依存 / 稀なメタデータ / 極端な低 fps)。しかし**全部が
「`-r` の値が誤る」クラス**で、その故障を検出できる唯一の検査を同じ commit が切ったため、
**軽微な既知バグが「無検出のデータ損失」へ昇格**している。

## 受け入れ条件

- [x] `__av1ify_frac_fps` / `__av1ify_detect_vfr` の awk に `LC_ALL=C` を付ける
- [x] `__av1ify_frac_fps` の分母 0 は「変換不能 = 空」に倒す (`24/0` を `24.000` にしない)
- [x] `-r` に丸めた 10 進でなく **`avg_frame_rate` の生の有理数** (`30000/1001`) を渡す (d と、
      NTSC が `2997/100` という非標準レートになる問題が同時に解消する)
- [x] `--fps N` 指定時のキャップ判定が VFR ソースで迂回されないようにする (c)
- [x] `_fps_changed` による**全か無かの抑制をやめ**、VFR 検出時は
      **期待フレーム数 = round(src_duration × avg_fps)** との密度比較に差し替える
- [x] e2e に**値の pin** を足す (`出力の r_frame_rate == ソースの avg_frame_rate`)
- [x] 上記すべてを変異検証し、**ケース名ごとの pass/fail** で判定する ([issue 399](399-chore-av1ify-test-harness-detection-power.md))

## 進捗

- `fix(av1ify,397,398,399): VFR 正規化の -r を誤らせる 4 経路を塞ぎ、密度検査で退行を観測可能にする`
  で受け入れ条件をすべて実装。`make test` はシェル系すべて緑
  (残る失敗 `test-unused-excluding-tests` は go 1.26.0 に staticcheck が未導入という環境要因で、
  **本体の checkout でも同じ失敗を再現**した = 本変更とは無関係)

### 結果 (変異検証。ケース名ごとの pass/fail で判定)

| 変異 | 結果 |
|---|---|
| M1 分母 0 の捏造を戻す | Test 6 の **3 assert すべて** red |
| M2 キャップを r のみへ戻す | Test 7 が red |
| M3 閾値を相乗りへ戻す | Test 8 / Test 9 が red |
| M4 `-r` を 10 進へ戻す | Test 2 / Test 10 が red |
| M5 密度検査を外す | Test 70d が red (`check_ng-density` にならない) |
| M7 `frac_fps` の `LC_ALL=C` を外す | Test 10 が red |
| M8 `-r 10` を注入 | **production の密度検査**が先に捕まえて red |
| M9 M8 + 密度検査も無効化 | **e2e の値の pin** が red (`got=10/1 want=77/3`) |
| M6 `is_vfr` の `LC_ALL=C` を外す | **green = 等価変異** (下記) |

M6 が green なのは、その awk が整数 `0/1` しか出さず `-v` の数値解釈がロケールで変わらないため
(実測 2026-09-19: de_DE でも `29.970` → 29.97)。**到達する全経路を確認したうえで等価変異と判定**し、
その事実をコード側のコメントに残した (テストで守られていないことを隠さない)。

### 副産物 (同じ修正の中で見つけて直したもの)

- 密度検査の初版は awk の組み込み関数名 `exp` を変数に使って **syntax error** になっており、
  `|| density_out=""` が失敗を空へ畳んで「判定不能」が「合格」に化けていた (検査が 1 度も発火しない
  fail-open)。変数名を直し、判定不能は stderr に出す第 3 の結果にした
- ロケールのテストが `locale -a | grep -q` で条件を書いており、`setopt pipe_fail` の下では
  `grep -q` が先に抜けて `locale` が SIGPIPE で死に、**常に無言でスキップ**していた

## 残タスク

- **2 周目の敵対レビュー** (`adversarial-review-own-safeguards.md` §7): 密度検査・閾値分離・
  assert ハーネスの変更は新設の安全機構なので、この差分にもう 1 周攻めさせる — **実施中**
- 症状そのもの (QuickTime での再生) は [issue 396](396-human-verify-quicktime-playback-after-vfr-fix.md) の目視待ちで**未検証のまま**

## 関連

- [issue 394](done/394-bug-av1ify-vfr-passthrough-breaks-quicktime.md) — 本 issue の対象実装
- [issue 396](396-human-verify-quicktime-playback-after-vfr-fix.md) — **そもそも症状が直ったかは未検証**
  (394 は当初「出力 DTS の非単調増加」を原因としたが 8504929d で誤りと判明し、現在の根拠は
  「旧出力が VFR」+「QuickTime との因果は目視待ち」)
- [issue 398](398-bug-av1ify-vfr-threshold-shared-with-frame-tolerance.md) — 検出閾値の相乗り
- [issue 399](399-chore-av1ify-test-harness-detection-power.md) — テストハーネス側の検出力

## スコープ外

- `--fps N` 併用時は `-fps_mode cfr` が一切付かないため、**VFR 正規化の射程は `--fps` 未指定時に限られる**。
  改修前からの挙動なので回帰ではないが、明記されていない (本 issue では c の修正のみ扱う)
