# 494 (perf): 演出の 1 コマで約 1.2MB を確保して GC が回り続ける (揺れ・カードの移動がたまにカクつく)

起票日: 2026-09-26

## 概要

ドッグフーディングで「バウンスやカードの移動がたまにカクカクする」(ユーザー)。演出のコマ (`ui/motion.go` の `frame`、33ms) の
処理を実測したところ、ループを長く止める処理は無かったが、**1 コマで約 1.2MB を確保し、1 秒の揺れのあいだに GC が 18 回回る**。
GC の mark を UI の goroutine が手伝わされる分だけ View が 5〜12ms に跳ねる (普段は 1〜4ms)。
確保の半分はカードの一覧をコマのたびに何度も作り直す `(*Model).columns` で、**カードの枚数に比例して増える** (完了のレーンは片付けるまで溜まる)。

## 実測 (2026-09-26)

条件: `~/.local/state/pro-con/live/cards.json` の写し (49 枚、うち完了 44 枚。レーンの枚数 `[0 2 0 1 2 44]`)、200×50、
Apple Silicon (14 コア)、Go の benchmark と `tea.NewProgram` を出力先 = 時刻を記録する writer で回した計測 (いずれも commit していない使い捨て)。
実端末・tmux を通した計測ではない。

| 何を | 値 |
|---|---|
| 揺れの 1 コマ (`Update(frameMsg)` + `View`) | 3.7〜3.9 ms / 1.47 MB / 5,060 allocs |
| 止まっているときの `render()` | 1.2〜1.5 ms / 0.81 MB / 3,845 allocs |
| 1 回の揺れ (30 コマ) | 確保 36.4 MB、GC 18 回、STW の合計 2.0 ms、HeapInuse 4.0 MB |
| 5 ms を超えた呼び出し (揺れとレーン移動を 20 回) | View だけ 5 回 (最大 12.3 ms)。Update は 0 回 |
| UI 側の poll (`Update(tickMsg)`) | 0.19 ms / 0.66 MB |
| 裏の読み直し `store.Load` / `store.LoadDerived` | 1.5 ms / 3 µs (別の goroutine。画面を待たせない) |
| コマ (View) から端末への書き出しまでの遅れ | 0〜20 ms にばらつく (描画は 60fps の別の ticker で書き出す) |
| レーン移動で画面に出る間隔 | 32〜35 ms で揃っている (30fps と 60fps のずれで 16/50 ms に割れる形は出なかった) |

確保の内訳 (揺れの benchmark の alloc_space、2 秒ぶん 1,206 MB):

- `(*Model).columns` 574 MB (47%)。呼び元は `positionOf` (`cursorTarget` 経由。Update の後の `trackCursor` と描画の `overlayCursor`) 241 MB /
  `boardLines` 116 MB / `trackLane` 110 MB / `overlayCursor` 106 MB。1 回の呼び出しで `card.Card` (960 バイト) を全枚数コピーして
  レーンに `append` し直し、並べ替える。1 コマで 5 回ほど呼ばれる
- 文字列の合成 (`splice` / `fit` / `ansi.Cut` / `ansi.Truncate` / `softSide` / `cellsOf`) が残りの大半。CPU でも、揺れのコマの `render` の
  6 割は `overlayBump` の行ごとの `fit` / `splice` (幅の計算で grapheme を走査する)

## 対応方針 (案)

1. **`columns()` を 1 回の Snapshot につき 1 回だけ作る**。結果を変えるのは `m.snap` と `m.tab` だけで、書き換えるのは `setSnap`
   (poll・起動・K / J の `moveCard`・`model.go` のもう 1 か所) と `ensureTab` / `moveTab` だけ (反証レビューで grep 確認)。ここで作り直し、コマの中では作らない。
   コピーするならカード本体ではなく添字か `*card.Card` にする。作り直しの契機を 1 か所へ寄せ、ほかから古い一覧を読めないようにする
   (`~/.claude/rules/survey-receiver-guards-before-passing-new-values.md` の「崩せる経路を全部挙げる」)
2. 文字列の合成で、変わらない行を毎コマ作り直さない。例: ボードの行を Snapshot と選択の変化のときだけ組み、演出はその上に重ねるだけにする。
   1 を入れた後に測り直して、まだ効くなら着手する
3. 効果は「1 コマの確保量」と「1 回の揺れの GC 回数」で before / after を測る (時間ではなく回数と量。benchmark をこの issue で置くなら
   カードの枚数を 50 / 200 と振って伸び率も見る)

## 見立てと未確認

- 🚨 **実端末でのカクつきの原因がこれだと確かめてはいない**。手元の計測では、コマの tick は 33ms で安定していて、画面に出る間隔も揃っている。
  「たまに」に当たりそうなのは GC による View の 5〜12ms の跳ねだが、それだけで 33ms の枠を越えるかは未確認
- 本番にだけある負荷は測っていない: 裏の読み直しが `claude agents --json` を起こす (最大 10 秒。`live/live.go` の `refresh`)、PG の session の
  node プロセス、tmux を通した書き出し。**1 を直してもカクつきが残るなら**、実際の起動で演出のコマの時刻と Update / View の所要を追記型のログに出して
  (環境変数で有効にする)、どの瞬間に遅れるかを見る (`~/.claude/rules/instrument-before-second-fix.md`)
- コマから書き出しまでの 0〜20ms のばらつきは bubbletea の構造 (モデルの tick と描画の ticker が別)。コマの位置は作った時刻で計算するので、
  見える位置が最大で半コマほど前後する。直すなら `frameInterval` を描画の周期 (16ms) に合わせる案があるが、効くかは未確認 (1 の後に判断)
- 上下の揺れが 65ms 刻みに見えるのは性能ではなく丸めの段数 (振れ幅 2 行で位置は 4 段。436)

## 関連ファイル

- `src/pro-con/ui/model.go` — `columns` / `visible` / `positionOf` / `Update` の後の追跡 (`trackCursor` / `trackLane`)
- `src/pro-con/ui/view.go` — `render` / `boardLines` / `fit`
- `src/pro-con/ui/cursor.go` — `overlayCursor` / `cursorTarget`
- `src/pro-con/ui/bump.go` — `overlayBump`
- `src/pro-con/ui/motion.go` — `frame` / `splice`
- `src/pro-con/live/live.go` — `refresh` (裏の読み直し)

## 進捗

- [x] 実測と原因の切り分け (上の表)
- [x] 反証レビュー (読み取り専用のサブエージェント 1 体): `columns` の 1 コマ 5 回・呼び元・カードのコピー・`claude agents --json` の最大 10 秒は反証されず。
  指摘 P3 (K / J は `setSnap` に含まれるので別の契機ではない) を対応方針 1 に反映。数値は再計測されていない
- [x] 1: 毎コマの経路からカード本体の複製をなくした (commit「pro-con: 演出の 1 コマでカード本体を複製しない (494)」)。
  「Snapshot ごとに 1 回作るキャッシュ」は採らなかった: テストや将来の経路が `m.snap` をその場で書き換えると古い一覧が残る、という新しい壊れ方を持ち込む。
  代わりに複製そのものをやめた:
  - `visible()` を `iter.Seq[*card.Card]` (m.snap.Cards の中を指す) に、新設の `lanes()` (レーンごとのポインタ) を毎コマの経路
    (`positionOf` / `boardLines` / `columnBlock` / `overlayCursor` / `slots` / `gauge`) で使う。`columns()` はキー操作の経路だけに残る
  - `columnCells` は見える `shown` 個だけセルを組む (それまで完了のレーンの全カードの文字列を毎コマ組んで捨てていた)
  - `backend.Snapshot.SlotsUsed` はカードを map へ複製せず PG ごとに探す (ゲージが描くたびに呼ぶ)
- [x] 3: before / after (同じ回で、実データの写し 49 枚・200×50):

  | | 直す前 | 直した後 |
  |---|---|---|
  | 揺れの 1 コマの確保 | 1.18 MB / 4,413 allocs | 0.42 MB / 3,206 allocs |
  | 1 回の揺れ (30 コマ) の GC | 15 回 (確保 32.8 MB) | 4 回 (確保 9.9 MB) |
  | 揺れの 1 コマの時間 | 1.79 ms | 1.29 ms |
  | 完了 50 → 500 枚の 1 コマの確保量の比 | 4.75 倍 | 1.06 倍 (タブを選んだときも 1.3 倍以内) |

  回帰の検査: `ui/alloc_test.go` の `TestFrameAllocDoesNotGrowWithCards` (比が 1.3 を超えたら落とす。global とタブの 2 腕)。
  `bin/mutate-verify` で、見えないセルも組む (1.43 倍) / `lanes` でカードをヒープへ複製 (3.12 倍) / `SlotsUsed` を map への複製に戻す (1.76 倍) /
  タブのときだけ `visible` が複製する (タブの腕 3.12 倍) の 4 本が red になるのを確かめた。検査が見ないもの (1 コマの外の経路・1 枚あたり小さい比例・
  枚数によらない確保・移動中の点線の枠の経路) はテストの冒頭に書いた
- [x] 敵対的レビュー (読み取り専用のサブエージェント、opus 1 体): 表示の変化・ポインタの寿命・`SlotsUsed` の意味の違いは壊せず。
  P2 (タブを選ぶと `visible` が複製する経路が検査の外) → タブの腕を足して直した。P2 (キー操作・Snapshot の差し替えの `columns()` は全枚数を複製する)
  → 1 コマの外なので記録のみ。P3 (同じ列に点線の枠が 2 つあると map の反復順で並びが揺れる) は既存の性質で今回の退行ではない (記録のみ)。
  修正の周は判定の新設がなく変異で直接確かめたので 2 周目は省いた
- [ ] 実端末でカクつきが減ったか (ドッグフーディングで確認。残るなら「見立てと未確認」の手順で本番のコマの時刻と所要をログに出す)
- [ ] 2: 行の作り直しを減らす (残りの確保は文字列の合成が中心: `cellsOf` / `softSide` / `splice` / `ansi.Cut`。実端末の結果を見て要否を判断)
