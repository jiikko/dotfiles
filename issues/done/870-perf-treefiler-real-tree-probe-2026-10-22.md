# 870 (perf): treefiler を実物の src で動かして測る道具 (tfprobe) と、そこで見つけたリーク・重さ

起票日: 2026-10-22

## 概要

ユーザー依頼「treefiler でリソースリークがないか調べて。パフォーマンス改善の余地も探して。src を実際に動かして、Claude が PDCA を
回せるように」。これまでの監査 (733 の perf・728 の保持) はコードを読むことと合成のベンチが中心で、本物の大きな木を本物の操作で
長く動かして資源と所要を数える手段が無かった。

- 道具: `src/treefiler/tools/tfprobe` (`make -C src/treefiler probe`)。filer の公開の面 (main.go と glogx が使うもの) だけで台本を回し、
  段ごとの 1 打・待ちの間のフレームの所要と、サイクルごとの goroutine・fd・子プロセス・GC 後のヒープを TSV に書く。使い方と読み方は
  `src/treefiler/CLAUDE.md` の「実物で測る」
- 見つけた重さ 2 件を直した (下の 1・2)。リークは見つからなかった (下の 3)

## 詳細

測定の条件: Apple Silicon (darwin/arm64)・go1.26.0・root = `~/dotfiles/src` (配下を全部開くと 193〜194 フォルダ)・画面 200x50。
1・2 の前後の比べは、道具の初期の版 (打鍵の間に時計を進めない・busy-close が無い) で、直す前と後のバイナリを同じ版の道具で続けて
回したもの (比は同じ条件どうし。絶対値は今の版の道具では再現しない)。3・4 の値は今の版 (2 周目の反証レビューの対応の後) の道具のもの。
**このマシンは load average が 4〜42 の間で揺れた** ので、前後の比較は「直す前のバイナリと直した後のバイナリを続けて、同じ条件で」回した
値だけを使う (load average は各比較の直前に 3〜4)。値はサイクル間の中央値。

1. **ライブ更新で巨大なフォルダの子が一斉に光ると、1 フレームが子の数の 2 乗** — `filer/model.go` の `drawRippleElbows`。光った項目ごとに
   親の行から子の行までの矩形を 1 セルずつ map で引いていた。木はカーソルの経路を画面の中ほどに固定するので、兄弟は画面の上下の外まで
   続き、i 番目の子は i 行ぶん空回りする (線の格子 `m.lines` は見えている行だけなので、外の行は空振りの map 参照)。
   直し方: 回す範囲を先に画面の行と桁に切る (外のセルは元々描かないので、描画は同じ)。
   - 実測 (`live-touch` = 3000 件の mtime を一斉に進めた後の待ちの間のフレーム、3 サイクル): **p95 141〜146ms → 6.5〜8.1ms**、
     最大 149〜166ms → 13〜15ms。CPU プロファイルでは前は `drawRippleElbows` が 41%
2. **コードのタイルで、読み足した 1 打が読めた行を全部まとめて色付けする** — `filer/tileview.go` の `view`。タイルを開いた時点で読めて
   いるのは先頭の 195 行 (model.go の場合) で、スクロールが読み足しに届いた 1 打 (118 打目前後) で残りの 1383 行が一度に chroma を
   通っていた (1 行約 33µs)。直し方: コードの色付けは描く行だけに描くときに付けて覚える (`tile.styledLine`)。コードの色付けは
   行ごと (折り返しなら 1 片ごと) に独立しているので、遅らせても同じ。Markdown (整形で色が付く) と diff (行をまたぐ状態) は先に付ける。
   - 実測 (`tile-code` = model.go を開いて 300 行スクロールし G / g、5 サイクル): **1 打の最大 44.26ms → 3.16ms**、
     段の確保 116MB → 93MB。代わりに待ちの間のフレームの p95 が 1.28 → 2.63ms (色付けが行の入るフレームへ移った分)
3. **リーク: 見つからなかった**。10〜20 サイクル (Model を作って閉じる = glogx の F の開閉) で goroutine・fd・子プロセスは横ばい
   (goroutine 1・fd 4〜6・子 0)。サイクルごとに「`e` と `r` を押した直後、裏の走査と git が走っている最中に閉じる」段 (`busy-open`) も
   回して同じ (Close は見張りしか止めないが、残った処理は終わって goroutine と子プロセスを手放す)。今の版の道具は閉じる直前の
   Busy() を確かめ (5 サイクルとも走っていた)、閉じた後に goroutine と子プロセスが開く前の数へ戻るまでを測る: 182〜246ms で戻った。GC 後のヒープのオブジェクトは 1 サイクルあたり十数〜数十増えるが、内訳 (全確保を記録したヒープの
   プロファイルの 5 サイクルと 20 サイクルの差) は次のとおりで、filer の状態を掴み続けるものは無かった
   - Go ランタイムの goroutine の管理構造 (呼び出し元の無い `runtime.mallocgc`)。同時に動いた goroutine の最大数で頭打ちになる
   - 道具自身の記録 (初版はサイクルごとに約 80 個溜めていたので、行をその場で書く形に直した) と `time.After` のタイマー
   - git を起こしたとき、`os/exec.(*Cmd).watchCtx` で作られる 112 バイトのオブジェクトが 1 サイクルあたり約 6 個 (約 0.7KB) 残る。
     git の repo でない木 (`/usr/share`) では出ない。**同じ形は filer を通さない最小のプログラム (下。filer と同じ WithTimeout +
     `subproc.CommandContext` と同じ WaitDelay) で再現する**: 残る数は 100 回 +148・2000 回 +612・4000 回 +870・8000 回 +1038 で、
     1 回あたりの増え方は 0.24 (100→2000 回)・0.13 (→4000 回)・0.04 (→8000 回) と鈍っているが、8000 回でも止まってはいない
     (`watchCtx` の分を区間で数えたのは 4000→8000 回の +156 だけ)。WaitDelay 0 でも 2 秒でも同じ。filer の 1 サイクルが git を何回起こすかとの
     数の突き合わせはしていない (定性の一致)。filer の書き方で避けられる形は見つからず、filer では直さない

     ```go
     // GOCACHE を分けて go build。引数: 回数 WaitDelay。前後に runtime.GC() を 2 回呼んで MemStats.HeapObjects を比べる
     func run(waitDelay time.Duration) {
     	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
     	defer cancel()
     	cmd := exec.CommandContext(ctx, "git", "--version")
     	cmd.WaitDelay = waitDelay
     	_, _ = cmd.Output()
     }
     ```
4. **フレームごとの線の格子の確保** (2026-10-22 の続きで直した): `move` (root の直下の列の中を端から端まで往復して 841 打) の確保が
   1 打あたり約 300KB あり、`-memprofile` の alloc_space で過半 (1 サイクル 287MB / 520MB) が `lineGrid.add` だった。線の格子
   (`filer/layout.go` の `lineGrid`) を毎フレーム新しい map で作り、セルごとに `&lineCell{}` を確保していた。セルを値で持ち、描画の格子は
   Model の `gridBuf` (`reusedMap`。大きな画面から戻ると作り直す) を毎フレーム使い回す (`lines` は新しい map を返すまま。テストが格子を
   持ち続けて比べるため)。回帰テスト `TestReusedLineGridMatchesFreshAfterCollapse` (畳んで描き直した格子が作り直した格子と同じ。前のフレームを
   空けない変異で red。消したフォルダの node を掴み続けるので leak_test の 4 本も red)
   - 実測 (同じ条件で前後のバイナリを続けて 5 サイクル): `move` の段の確保 255MB → 97MB・1 打の p95 1.27 → 1.13ms、`search` の段 18.8MB → 6.7MB、
     `tile-code` 93MB → 45MB、`explode` 7.6MB → 6.0MB
   - 1 フレームの確保の回数 (`filer/alloc_budget_test.go`): 255 → 108 回。上限を 260 → 112 に締めた。glogx の `frame_alloc_test.go` の
     filer の行も 220 → 124 回 (-race なし)・50799 → 39790 B/frame (-race) で、上限を 222 / 51500 → 126 / 40500 に締めた。map を毎フレーム作り直す変異で
     119 回、セルごとに確保する変異で 301 回 (glogx は 232 回) → どちらも red
5. **記録 (直していない。測った値)**:
   - `search` (列の中の `/tr` → 確定 → `n` 30 回) の 1 打は p50 0.84〜0.94ms
   - **初版の台本の move / search は空打ちだった**: どちらもカーソルの列 (兄弟) の中で動くが、台本は root (列は root だけ) にカーソルを
     置いたまま打っていた。その版の値 (move p50 0.6ms・1 打 155KB など) は何も測っていない。段の後の状態の列 (カーソル) と煙テストで
     見分けるようにした (反証レビューの指摘)
   - `r` (Refresh) の 1 打が 7〜11ms、`e` (配下を全部開く) の後のフレームの最大が 9〜14ms。`live-touch` のフレームは 1 を直した後も
     p95 6.5〜8.1ms (3000 件の波紋を毎フレーム回す)。8000 件 (5000 件を超えると見張りが間引く経路) では p95 3.8〜21.6ms
     (2 サイクル。load average 25 前後で揺れが大きい)
   - 単体の treefiler (`main.go`: tea.Exec・toast) と glogx の画面の層は台本の外

## 対応方針

1・2・4 は直した。3 はリークではない (git の分は filer の外でも同じ形で出て、増え方は鈍っていく)。5 は 1 フレーム 16ms の中に収まっていて重さの訴えも無いので
直さない。同じ形の重さを探すときは、まず `make -C src/treefiler probe` の `worst_at` (最悪の 1 打の名前) と `-cpuprofile`、確保なら
`-memprofile` の `-sample_index=alloc_space` を見る。

作業中に見つけた別の問題: 仮想化の等価性のテストが負荷の下で揺れる → issue 871。

再開の条件 (3): tfprobe の heap_objects の増え方が 1 サイクルあたり 100 を超えたとき (git の分は 1 サイクル約 6 個なので、超えたら別のもの)。
そのときは `-memprofile` の 2 本の差 (`go tool pprof -sample_index=inuse_objects -base`) から始める。

## 関連ファイル

- `src/treefiler/tools/tfprobe/main.go` / `main_test.go` (台本が空振りしていないことの煙テスト)
- `src/treefiler/filer/model.go` の `drawRippleElbows` と `drawLines` / `src/treefiler/filer/tileview.go` の `view` と `styledLine` /
  `src/treefiler/filer/layout.go` の `lineGrid` と `linesInto`
- `src/treefiler/CLAUDE.md` の「実物で測る」
- 733 (前回の perf 監査)・728 (保持の監査)

## 進捗

- [x] tfprobe を作る (台本・段の後の状態で空振りを見分ける・サイクル間の中央値・資源の推移・pprof)
- [x] 1 を直す。回帰テスト `TestRippleElbowClipMatchesUnclipped` (切らない回し方を正解役に全セルを比べる。切る範囲を 1 行ずらす変異 2 本で red)。
  速さだけの退行 (切り詰めを外す) はテストで固定していない (壁時計を合否にしない)。tfprobe の `live-touch` で見る
- [x] 2 を直す。回帰テスト `TestCodeHighlightOnlyForDrawnLines` (色付けの回数が描いた行の数で済む。先に全行を色付けする変異で 3000 回 → red)
- [x] 3 のリークの調べ (結論は上)
- [x] 反証レビュー 2 本 (opus: 計測の妥当性と issue・sonnet: 2 つの修正)。P1 なし。直したもの: open の行に一時フォルダの木が混ざる
  (段名を分けた)・裏の処理の最中に閉じる経路が無い (`busy-open`)・打鍵の間に時間が進まない (Model に見せる時計を 1 打 16ms 進める)・
  move / search の空打ちと、live-touch の確認が状態の列で見分けられない (台本と煙テスト)・1 サイクルのときの増分が 2 秒待ちの差になる・
  タイルが開かないと木の上で d がゴミ箱の確認を開く (開いたことを確かめてから押す)・等価テストが横の切り詰めを当てない
  (カメラをずらした組を足した)・色を覚える部分のテストが無い (2 回目の描画で付け直さないことを足した)。
  記録のみ: 5000 件を超える間引きの経路は既定の台本の外 (`-live 8000` で測る)
- [x] 直した差分への反証レビュー 2 周目 (opus)。P1 なし。直したもの: move / search の空打ちがまだ煙テストを通る (move を列の最後で止め、
  move と search の後のカーソルを名前で比べる)・busy-open が本当に裏の処理の最中に閉じたかを確かめていない (閉じる直前の Busy() を書き、
  false なら空振り)・閉じた後の残りの処理が後の段の計測と資源の基準の行に混ざる (goroutine と子プロセスが戻るまで待ち、その時間を
  busy-close の行に書く。戻らなければリークの候補として止める)・ライブ更新を取り込むフレームが記録から外れていた・値の道具の版の明記。
  move の j / search の / を効かなくする変異で煙テストが red。煙テストは 3 回続けて緑
- [x] 2026-10-22 (続き。ユーザー依頼「870 をやって」): 3 の未解明を最小のプログラムで切り分けた (Go の側・頭打ちに近い)。4 の線の格子の確保を
  直し、filer と glogx の確保の予算を締めた。`TREEFILER_VIRT_SEEDS=300` で仮想化の等価性を 2 回回して緑 (1 回目の 1 本の赤は揺れ → 871)。
  反証レビュー (opus): P1 なし。直したもの: 使い回しの経路を直接守るテストが無い (足した)・容量の残る map を素の clear で使い回していた
  (既存の reusedMap に寄せた)・予算の注記の -race の書き方・3 の「頭打ち」の言い過ぎ・871 の対応方針 (下)
