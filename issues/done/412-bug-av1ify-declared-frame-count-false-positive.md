# 412 (bug): av1ify postcheck が壊れた AVI のヘッダ水増し (`nb_frames`) を実データ欠落と誤検出する

起票日: 2026-09-23

## 概要

`frames` / `duration` / `vidloss` の3検査がすべて、コンテナが自己申告する
メタデータ (`nb_frames` / `format=duration` / `stream=duration`) を実データと
取り違えて誤検出することがある。実ファイル (私物、パスは省略) で実測した。

- `frames` チェックは `stream=nb_frames` を無条件に信用しており、フォールバックが無い
- `vidloss` チェックは閾値超過時に `__av1ify_packet_end` で再測定してから降格判定するが、
  この再測定自体が壊れたコンテナでは**宣言値と同じ誤った値を再現する**ため降格が働かない
  (issue 397 が作った安全網が、まさにそれが守るはずのケースで機能していない)
- `duration` チェック (`format=duration` の突合) には再測定フォールバックが元々無い

## 詳細

### 実測 (private な AVI ファイル、mpeg4/mp3, 30000/1001fps, 約2時間35分)

```
declared nb_frames (src, stream=nb_frames)     = 280,661
declared duration   (src, format=duration)      = 9364.722033s
__av1ify_get_stream_end(src, v:0)               = 9364.722033s (declared と同値)
__av1ify_packet_end(src, v:0)  ← vidloss の再測定に使われる関数
                                                 = 9364.722034s (declared とほぼ同値。壊れている)

実パケット数 (ffprobe -count_packets, 全走査・seek なし)
  src: nb_read_packets = 278,888
  out (av1ify の出力): nb_frames = 278,888  ← 完全一致

全走査 (packet=pts_time を先頭から末尾まで、-read_intervals 無し) の最終 pts_time
  src = 9312.670033s  ← out (9312.670033s) と完全一致
```

つまり **実データは 278,888 フレーム / 9312.67秒しか無く、av1ify の出力はそれを
漏れなく変換できている**。ヘッダの `nb_frames`(280,661) と `format=duration`(9364.72s) は
実データより 1,773 フレーム (≈52秒) 水増しされた値で、この AVI 固有の破損
(おそらく不完全なダウンロード後に index/header だけが元の想定値のまま残った状態。
実データ自体には `corrupt decoded frame` 等のマクロブロック破損も別途あるが、
それは今回のフレーム数不一致とは無関係)。

### なぜ `vidloss` の再測定フォールバックが機能しなかったか

`__av1ify_packet_end`(`zshlib/_av1ify_postcheck.zsh:99-138`) は大きいファイルでの性能のため、
`format=duration`(壊れた宣言値) を基準に「末尾60秒」だけを `-read_intervals "<start>%"` で
seek して読む最適化を持つ。今回のように **idx1 が実データを超えて水増しされた AVI** では、
この seek が実データの終端 (9312.67s) を素通りして存在しないインデックスエントリへ着地し、
`format=duration` に近い値 (9364.72s) をそのまま返してしまう。これは
「宣言 duration が不正確なケースを実測で救う」という issue 397 の設計意図そのものが
無効化される形で、**再測定のはずが宣言値の再生産になっている**。

一方、`-read_intervals` を使わない**全走査**（`ffprobe -count_packets` または
`-show_entries packet=pts_time` を seek なしで先頭から流す）は正しい値
(278,888 / 9312.67s) を返す。全走査は demux のみでデコードを伴わないため、
1.5GB クラスのファイルでも 1秒未満で終わる (実測: 0.55秒)。

### `frames` チェックにはそもそも再測定が無い

`zshlib/_av1ify_postcheck.zsh:566-568` の `src_frames=$(__ff_stream_field ... stream=nb_frames)`
は宣言値を無条件に信用しており、`duration`/`vidloss` のような「閾値超過時だけ実測で
再判定する」フォールバックが存在しない。

## 対応方針 (codex 反証レビュー後に改訂)

起票時点の対応方針は「実パケット数 (映像のみ) が一致したら 3 検査を一括降格する」
だったが、commit 前の codex 反証レビューで P1 2 件・P2 4 件の指摘を受け、
**検査ごとに独立した、その検査が実際に見ている量と同じ種類の再測定**へ設計を変更した
(指摘内容は下の「codex 反証レビュー」節)。

- 新規ヘルパー `__av1ify_count_packets(file, spec)`: `-count_packets` による全走査
  (seek 無し)。**`frames` チェック専用**。閾値超過時だけ呼ぶ
- `__av1ify_packet_end(file, spec, force_full=0)`: 既存関数に第3引数を追加。
  `force_full=1` で末尾区間の seek 最適化を丸ごとスキップし、最初から全走査する
  (既存呼び出しはすべて2引数のままで、デフォルト値により挙動は変えない)
- `frames`: 宣言 `nb_frames` の不一致が閾値超過のとき、`__av1ify_count_packets` で
  src/out の実パケット数を再測定し、一致すれば降格
- `vidloss`: 既存の「packet 実測で再判定」(issue 397) がそれでもまだ不一致を示すとき
  (= 区間 seek の再測定自体が壊れている疑いがあるとき) だけ、`force_full=1` で
  全走査に強制した最終確認を追加する。一致すれば降格
- `duration`: 閾値超過時、**映像・音声それぞれ**の全走査終端 (`force_full=1`) から
  真の「長さ」= `max(映像終端, 音声終端)` を測り直し、出力側の宣言 `format=duration`
  と比較する。音声ストリームが存在するのに実測できないときは判定不能として降格しない
  (codex P1: 映像パケット数だけで duration を降格すると、映像は無傷で音声だけ欠落した
  ケースを隠す。duration チェックはそもそもそれを検出するために存在する検査
  — この block の直上のコメント参照)
- **`density` (fps 変更時の検査) はスコープ外**。閾値・suffix が異なり
  (`AV1IFY_DENSITY_FLOOR`/`AV1IFY_DENSITY_TOLERANCE_PCT`)、`AV1IFY_FRAME_TOLERANCE` を
  再利用しない (codex P2: 通常フレーム検査の閾値が fps 変更時の安全機構に波及するのを防ぐ)
- 実測・再測定自体が失敗した場合は何も降格せず、既存の NG を維持する
  (fail-safe: vidloss の既存 downgrade と同じ方針)

## codex 反証レビュー (commit 前、起票時点の設計に対して実施)

`tmp/codex_review/prompt_issue_412.md` / `result_issue_412.md` (このセッションの `tmp/`。
セッション終了後は残らないため要点のみここに転記)。

- **P1: `duration` を映像パケット数だけで降格すると音声欠落を隠す** →
  対応方針を検査ごとの独立した再測定に変更し、duration は音声も見るよう修正
- **P1: パケット数一致は「実フレーム完全一致」の一般的な証明にならない** →
  `frames` チェック専用の再測定として scope を絞った (duration/vidloss には
  パケット数を流用しない。それぞれ自分自身の量 (終端時刻) で再確認する)
- **P2: 3検査が同じ宣言メタデータに依存するという記述は不正確** (`vidloss` は
  `stream=duration` が無ければ既に `__av1ify_packet_end` の全走査にフォールバックする) →
  本文の記述を訂正 (上の「なぜ vidloss の再測定フォールバックが機能しなかったか」節は
  「区間 seek が壊れている」ケースに限定した説明であり、関数全体の一般化ではない)
- **P2: `frames` チェックにフォールバックが無い、は `fps_changed=0` の経路に限定すれば正しい**
  (fps 変更時の密度検査には既に複数の防御がある) → 受け入れ条件・対応方針で明示的に
  `fps_changed=0` の通常 `frames` チェックだけを対象と書いた
  (density は別スコープであることを追記)
- **P2: `AV1IFY_FRAME_TOLERANCE` を density 側にも再利用する根拠がない** →
  density には触れない設計に変更 (上記)
- 反証できなかった主張 (私物ファイルでの実測値、`__av1ify_packet_end` の区間 seek が
  実際に壊れた値を返したこと) はそのまま採用

## 受け入れ条件

- [x] `__av1ify_count_packets` を追加 (全走査、seek 無し。`frames` チェック専用)
- [x] `__av1ify_packet_end` に `force_full` 引数を追加し、`vidloss` の再測定を
      全走査で最終確認できるようにする (既存呼び出しの挙動は変えない)
- [x] `frames` が NG のとき、実パケット数で再確認し一致すれば降格する
- [x] `vidloss` が区間 seek の再測定後もまだ NG のとき、全走査で最終確認し
      一致すれば降格する
- [x] `duration` が NG のとき、映像・音声それぞれの全走査終端で再確認し、
      音声も含めて一致すれば降格する (音声だけの欠落は降格しない)
- [x] 実パケット数 / 終端が実際に不一致 (本物の欠落) のケースでは降格しないことを確認する
- [x] `tests/zshrc/av1ify/test_helper.sh` の mock ffprobe に `nb_read_packets` 応答と、
      `-read_intervals` の有無で区間 seek 応答と全走査応答を分けて返す仕組みを追加
      (未設定時は従来どおり同じ値を返すので既存テストの挙動は変えない)
- [x] 新規テスト (Test 91-96 in `test_av1ify_postcheck.sh`, Test 11-12 in
      `test_av1ify_video_duration.sh`): 各検査の降格・非降格・fail-safe・
      codex P1 (音声欠落は隠さない) を個別に確認 + 実際の事故値そのものを使った
      統合テスト (3検査すべてが同時に降格されることを確認)
- [x] 変異検証 (`bin/mutate-verify` で5パターン。降格ロジックを外す/常に降格させる
      変異、duration が音声を無視する変異、いずれも想定どおり対象テストが red)
- [x] 既存テスト (`make test-dir DIR=tests/zshrc/av1ify`、16ファイル741 assertion) が green
- [x] shellcheck / `zsh -n` 構文検査 green
- [x] 敵対的レビュー (安全機構の変更のため最終ゲート。最終 diff に対して実施。下記)

## 敵対的レビュー (最終 diff に対して実施)

codex に red team として「偽陰性を作れるか / fail-safe が本当に fail-safe か / 既存ロジックとの
相互作用 / 性能 / テストの検出力」の6観点で壊しにいかせた。**P1 は 0 件**。

- **P2: `frames` の再測定が測っているのは「パケット数」で「フレーム数」ではない**
  (`nb_read_packets` ≠ `nb_frames` になりうるコーデック/コンテナが存在する) → 対応せず、
  既知の限界として `__av1ify_count_packets` のコメントと下の「残る既知の限界」に明記した
  (理由: 全走査でのデコードベースのフレームカウントは、遅いことに加えて、この issue の
  実ファイルのようにビットストリーム自体が別途破損している場合はデコード結果自体が
  信用できない。意図的にデコードを避ける設計とのトレードオフ)
- P3 (テストの検出力に関する3件) は反映: `__av1ify_count_packets` にコメント追記、
  `vidloss` Test 12 の audio mock 値を揃えて avsync の横入りを防いだ (Test 91/92/93 等は
  既に単独主張になっていたので変更不要)。Test 96 が「降格前に NG が出ていたこと」を
  直接 assert していない点は、Test 91/93/Test 11 が個別に同じ削除値で NG が出ることを
  既に証明しているため許容 (低リスクな重複削減)
- 確認できたこと: force_full=1 は既存呼び出し (avsync 等) に波及しない / 正常系の
  変換パスに追加コストは無い / 実測失敗時は fail-safe (降格しない) / 本物の欠落を
  作る既存の回帰テストは全部生存 (壊せなかった)

## 残る既知の限界 (対応しないと判断したもの)

1. **`frames` チェックの再測定は packet 数であり frame 数の保証ではない**
   (敵対的レビュー P2)。映像ストリームでは 1 packet = 1 frame が一般的だが、
   理論上は崩れうる。デコードベースの真のフレームカウントは遅く、かつビットストリーム
   自体が破損している場合は信用できないため、意図的に採用しない
   (issue 397 の「残る既知の限界」と同種の判断)

## 関連ファイル

- `zshlib/_av1ify_postcheck.zsh` — `__av1ify_packet_end` / `__av1ify_postcheck` の
  frames/duration/vidloss 各チェック
- `tests/zshrc/av1ify/test_av1ify_postcheck.sh` / `tests/zshrc/av1ify/test_av1ify_video_duration.sh` /
  `tests/zshrc/av1ify/test_helper.sh`

## 進捗

- 2026-09-23: 起票。私物ファイルでの実測 (良い/破損の両方) と原因特定 (seek ベースの
  再測定自体が壊れている) まで完了。codex 反証レビュー (P1 2件/P2 4件) を受けて設計を
  検査ごとの独立した再測定へ改訂。実装・新規テスト10本・変異検証5パターン・敵対的
  レビュー (P1 0件) まで完了
- 2026-09-23: 「fix(av1ify,412): 壊れたコンテナのメタデータ水増しを実データ欠落と誤検出しないようにする」で commit 済み。受け入れ条件すべて充足のため done へ移動

## 関連

- [issue 397](397-bug-av1ify-vfr-wrong-r-value-undetected-frame-loss.md) —
  `frames` チェック (密度検査) の元々の実装。「残る既知の限界 1」で
  「vidloss 判定との尺の解釈の食い違い」を残課題として明記しており、本 issue はその
  具体化 + `vidloss` 自身の再測定 (`__av1ify_packet_end`) が信頼できないケースの発見
- [issue 395](../pending/395-feat-av1ify-repair-mode-for-corrupt-sources.md) —
  破損ソースの修復モード (pending)。本 issue は「実データは揃っているのに誤検出される」
  ケースで、395 が扱う「実データが本当に壊れている」ケースとは別
