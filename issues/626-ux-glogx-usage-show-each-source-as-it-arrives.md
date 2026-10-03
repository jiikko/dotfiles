# 626 (ux): glogx の利用枠 (U / R) を、Claude と codex の両方がそろうのを待たず、取れた方から出す

起票日: 2026-10-02

## 概要

glogx の利用枠の表示 (右上の U の箱 / 全画面の R のダッシュボード) は、`usage.FetchAll` が Claude と codex を並列に取ったあと
両方の結果を 1 つの Snapshot に併合してから 1 通の `usageMsg` で届く。速い方 (codex app-server は 0.6〜1.2 s、claude は約 2 s。
`usage_overlay.go` の fetchCmd のコメントの実測) が先に返っても、遅い方 (最大 `fetchTimeout` = 10 s) を待ってから描かれる。
ユーザーの要望 (2026-10-02): 取得した方から表示していく。

## 対応方針

- usage パッケージ: 出所ごとの取得 (Claude / codex) と、出所ごとの結果を Snapshot へ入れる併合を 1 か所に置き、`FetchAll` もそれで組む
  (bin/ratelimit と hook の振る舞いは変えない)
- glogx: fetchCmd は 2 本の取得を同時に投げ (Cmd が `tea.BatchMsg` を返す)、`handle` は届いた出所の枠だけを入れ替える
  - last-good は出所ごと (今の `MergeLastGood` と同じ不変条件)。全滅 (両方失敗) の扱い・`staleErr` / `ClaudeErr` の注記は今と同じ意味を保つ
  - single-flight は「その周の 2 本が両方返るまで」。キャッシュの保存は周の終わりに、その周で取れた分だけで行う (Claude 必須の契約は今のまま)
  - 片方を待っている間は、待っている出所を「取得中」として出す

## 進捗

- 2026-10-02: 起票
- 2026-10-02 「feat(glogx): 利用枠を Claude と codex の取れた方から出す (626)」
  - usage: 出所ごとの取得 `FetchClaudePart` / `FetchCodexPart` と、出所ごとの結果を入れる `Snapshot.With` (書き換えずに新しい値を返す。
    出所ごとの last-good・ClaudeErr・Claude → codex の並び) を置いた。`FetchAll` / `MergeLastGood` は本番の呼び出し元が 0 になった
    (bin/ratelimit は `Fetch` / `FetchCodex` を直接呼ぶ) ので消し、その stub テストは Part の形へ移した
  - glogx: fetchCmd は 2 本を `tea.BatchMsg` で投げ、handle は届いた出所を `usageRound` (周の始まりの表示 + 届いた分) から表示へ入れる。
    初回の取得だけ、まだの出所を U の箱の行と R の見出しの下に「⠋ Claude Code 取得中...」と出す (定期リフレッシュでは出さない)。
    キャッシュの保存は周の終わりに、その周で取れた分だけ (Claude 枠があるとき) を書く。全滅の周は周の始まりの表示へ戻して staleErr
  - 変異 (すべて red、mutate-verify-list): ClaudeErr を立てない / 消さない / 並びを codex 先に / 周の途中で表示へ入れない /
    何も取れていないのに表示を作る / 全滅で戻さない / 理由を届いた順に / 保存に last-good を混ぜる (2 通り) / リフレッシュで「取得中」/
    箱の「取得中」の行を出さない / スピナーが待ちを見ない / 盤の描画キャッシュの鍵から待ちを外す / tui が待ちを渡さない /
    R のスピナーが待ちを見ない / 回復の err を下ろさない / staleErr を早く消す・消さない / codex の失敗を ClaudeErr に / codex のバージョンを落とす
  - 敵対的レビュー (opus、2 周): 1 周目 P2 3 件 = テストの穴 (部分の回復・codex の失敗で ClaudeErr・保存の last-good) → テストを足して変異で red。
    P3 = 全滅の後の周で先に届いた方が staleErr を消し、Claude の前回の値が注記なしで最大 10 s 出る (再現あり) → staleErr は周の終わりに下ろす形に直した。
    P3 = usageMsg に周の番号が無い → 閉じた周の後に part が届く経路は今は無い (endRound は 2 本そろったときとキャッシュの hit だけ) ので直さず、
    handle の doc に「経路を足すなら先に番号を持たせる」と書いた。コメントの古い記述を直した。
    2 周目: 壊せなかった。P3 = 周の途中で codex が新しくても「前回の値を表示中」が残る (過剰な警告の向きなので受ける) /
    codex のバージョンが未検査 (変更前から) → 検査を足した
  - `make -C src/glogx test` / `lint`、`make -C src/ratelimit test` / `lint` rc=0。`-race -count=3` (Usage / FetchCmd / RLDash / Ratelimit) rc=0
  - root の `make test` は rc=2: 落ちたのは `tests/bin/test_kernel_alloc_watch.sh` の 1 件だけ (「ロックを握る側が 20 秒たってもロックを取れない」)。
    並列の負荷の下でだけ出て、単体では rc=0。今回の変更 (src/glogx・src/ratelimit) は触っていない
  - 残り: 実機の glogx での見え方 (起動直後に codex が先に出て、Claude が後から入る) は未確認
- 2026-10-02 「feat(glogx): 利用枠の取得中も、届いた後と同じ幅と段を取る (626)」 (ユーザーの追加の要望: 取得前と後でレイアウトの幅を変えない)
  - usage: `Window.Pending` (表示だけ。キャッシュへ書かない) と `PendingWindows(src, like)`。Claude は 5h / 7d、codex は前回のキャッシュ
    (TTL 切れも使う。glogx の `loadUsageShape`) の枠の構成を写し、無ければ cx7d。前回の値 (Percent / ResetAt / Unused) は表示しない目安として持つ
  - 表: 列の幅を値の最大 ("6日" / "23時間" / "59分" / "12月" / "31日") から始める (今までの表示も 1〜2 桁広がる)。場所取りの行は「取得中...」だけを同じ幅で出す
  - 盤: 場所取りのカードは「取得中...」の本体 + 空の下の段。見出し (AA か 1 行か) と下の段の行数は、目安の値で実物と同じ組み方をして決める (`cardFrame` に切り出した)
  - glogx: `usageOverlay.view()` が最初の取得の間だけ場所取りを入れ、U の箱と R の盤はこれを描く。箱の後付けの「取得中」の行はやめた (行が出入りして高さが変わるため)
  - 変異 (すべて red): 場所取りを入れない / 箱・盤が view を描かない / 列の幅を値から決める / 区切りを残す / 時刻の列を空けない /
    下の段を固定の行数にする / 本体に盤を描く / Claude の場所取りを 1 枠にする / 前回の値を写さない / codex の形を写さない /
    glogx が形を読まない / 形の読み込みが TTL を見る / 未消費の目安を落とす / 窓幅 0 を既定へ戻さない。
    cardPace の Unused と Pending の順の入れ替えは緑 (場所取りのカードは語を描かないので等価) で、順は元に戻した
  - 敵対的レビュー (opus、2 周): 1 周目 P2 = 盤のカードの見出しが場所取り (使用率 0) と実物で入れ替わる → 前回の値を目安にし、さらに下の段の行数も
    目安の値で組むようにした (使用率の代表値 50 では字形の幅の差で直らなかった)。P2 = codex が 2 枠のプランで行が増える → 前回の枠の構成を写す。
    2 周目: 漏れ・panic は壊せず。P2 = 前回も今回も未消費の 5h で下の段の形が変わる → Unused も目安に残す。P3 = 窓幅 0 のキャッシュ → 既定の窓幅へ。doc のずれを直した。
    最後の修正はどれも変異で直接 red を確かめたので 3 周目は打ち切った
  - 避けられない限界 (直さない): 取得に失敗した出所の行は消える (箱の行の数が減る) / 前回の値と今回の値が大きく離れる・前回が無いと盤のカードの中の形が
    変わりうる (段の位置とカードの配置は変わらない) / 「6日」より長い値・ラベルが来たら列が広がる
  - `make -C src/glogx test` / `lint`、`make -C src/ratelimit lint` rc=0、`-race -count=3` rc=0。`make -C src/ratelimit test` は
    `TestFetchCallerTimeoutIsNotShared` (shared_test.go。627 の共有ゲートのテスト) だけが落ちる。変更していない master でも落ちる (627 の持ち主へ連絡済み)。
    root の `make test` は `tests/bin/test_kernel_alloc_watch.sh` だけが落ちる (前の commit と同じ負荷の偽の赤。単体では rc=0)
- 2026-10-02 「feat(glogx): 利用枠の場所取りにスピナーを添える (626)」 (ユーザーの追加の要望: 表示前は回るインジケータを出す)
  - `Window.Spinner` (表示だけ) を `pendingText` が「⠋ 取得中...」にする。U の箱の行と R の盤のカードに出る (幅は「取得中...」の列の中に収まる)。
    glogx の `view(spinner)` が毎フレームのコマを入れ、`loading()` は片方を待つ間も真に戻した (tick を回してスピナーを動かす)
  - 変異 (すべて red): view がコマを入れない / loading が待ちを見ない / 表がコマを添えない / 盤のカードがコマを添えない
  - 敵対的レビューは省略: 判定ロジックの新設は無く (表示の文字列と、レビュー済みの loading の条件へ戻しただけ)、各変更を変異で直接確かめた
  - `make -C src/glogx test` / `lint`、`make -C src/ratelimit lint` rc=0

- 2026-10-02 (別セッションから追記): 上で「無関係」とした 2 つの落ちる原因が分かった。どちらも 626 の変更とは無関係。
  `tests/bin/test_kernel_alloc_watch.sh` はロック待ちの判定の `lockf` に `-k` が無いこと ([630](done/630-test-kernel-alloc-watch-lock-probe-unlinks.md))。
  `TestFetchCallerTimeoutIsNotShared` は stub の `sleep` が `PATH` から見つからないこと ([627](done/627-bug-claude-usage-polling-hits-429.md) の進捗の末尾)
- 2026-10-03 (別セッションから追記): 上の `test_kernel_alloc_watch.sh` の落ちる原因は 630 で解消 (判定の `lockf` を `-k` 付きにした。CI の Tests run 37034308994 で緑)
- 2026-10-03 (別セッションから追記): 上の `TestFetchCallerTimeoutIsNotShared` の落ちる原因は 627 側で解消 (stub の `sleep` を `/bin/sleep` に。[627](done/627-bug-claude-usage-polling-hits-429.md) の進捗の末尾)
- 2026-10-03 実機の見え方を確かめた (隔離した tmux `-L` で HEAD の glogx を 120x40 で起動し、起動時のグランスを 20 ms ごとに `capture-pane`。
  `XDG_CACHE_HOME` を使い捨てに向け、本物のキャッシュを写して `fetchedAt` を古くした = 形は前回のもの、取得は必ず走る)。3 回撮った:
  - ✅ 出所が届いた後: Claude が先に届くと、codex は前回の形の 2 行 (`cx5h` / `cx7d`) が「⠋ 取得中...」の場所取りになる。
    箱の幅 (60 桁) と行数は codex が届いても変わらない。スピナーも回る
  - ✅ 前回のキャッシュに codex の枠が無いと、codex の場所取りは `cx7d` の 1 行で、届くと `cx5h` / `cx7d` の 2 行に増える (仕様どおり。進捗の「避けられない限界」と同じ)。
    本物の `~/.cache/glog/claude-usage.json` は撮った時点で Claude の枠しか持っていない回があった (codex が取れなかった周の保存)
  - 🔴 **何も届いていない最初の約 0.5 秒** (3 回とも 22〜27 コマ): 箱が幅 22 桁・1 行の「┌ Claude Code · usage / ⠋ 取得中...」になり、
    Claude が届いた瞬間に 60 桁・5〜6 行へ広がる。原因: `usageOverlay.boxLines` の `case o.snap == nil:` が 626 より前からある 1 行の分岐で、
    `view()` (場所取り) を通らない。`waitingSources` も `o.snap == nil` で空を返す。2 つ目の commit の要望 (取得前と後でレイアウトの幅を変えない) の
    いちばん目に付く場面 (起動直後) が満たされていない。R の盤の `snap == nil` (`usage_overlay.go` の `if snap == nil {` / `tui.go` の toggleRatelimitDash) は未確認
  - 残り: 上の 🔴 を直す (snap が nil でも最初の取得の間は Claude / codex の場所取りを描く)。直したら同じ撮り方で取得前のコマを撮り直す → 下で解消
- 2026-10-03 「fix(glogx): 利用枠の最初の取得で何も届いていない間も場所取りを描く (626)」 (ユーザーの依頼: 直して、撮り直してから done へ)
  - U の箱と R の盤はどちらも `usageOverlay.view()` を描くので、`waitingSources` の 1 か所で両方の穴だった。
    snap が nil でも最初の取得中は場所取りの対象を返す (初回の全滅の後 = err ありは返さず「取得失敗」を保つ)。boxLines の分岐も view を見る
  - 敵対的レビュー (opus) 1 周目 P2 2 件を直した: 何も届かないうちに Claude が先に失敗すると Claude の段が注記なしで消える
    (→ view が届いた失敗を土台に畳んで ClaudeErr の注記を出す) / codex 未導入の環境で起動のたびに codex の場所取りと「+ codex」が出て消える
    (→ 前回の形に codex の枠が無く `lookPathFn("codex")` も失敗するなら codex の場所取りを置かない。判定は `loadUsageCache` と同じ)
  - 2 周目: P1/P2 なし。P3 は記録だけ (直さない):
    - P3-a: codex 未導入で、Claude の失敗が codex の失敗より先に届くと、その間だけ箱が 1 行の「取得中...」に戻る。両方の CLI がすぐ失敗し、かつ
      goroutine の順が逆転したときだけで、続くのはミリ秒。trigger: 起動直後に「取得中...」の 1 行が目に見えて残る報告が出たとき
    - P3-b: R の盤の 2 行目は、Claude が失敗して codex を待つ間「codex 取得中...」より ClaudeErr の注記を優先する (`lines` の head[1])。
      codex のカードにスピナーが付くので待ちは見え、形も変わらないので受ける
    - P3-c: codex は入っているが毎回失敗する環境 (未ログイン等) では、起動のたびに codex の場所取りが出て消える。`loadUsageCache` が毎回 miss する
      環境で、この修正の前から同じ。trigger: そういう環境で使うことになったとき (codex の失敗の種類で判定する必要がある)
  - 変異 (mutate-verify、すべて想定の検査で red): 条件を元に戻す / 箱の分岐を戻す / 全滅の後も出す / 失敗の畳み込みを外す /
    noCodexPending を無視 / 前回の形の codex 判定を外す。`make -C src/glogx test` / `lint` rc=0
  - 撮り直し (前と同じ撮り方、3 回): U の箱は最初のコマから「5h / 7d / ── / cx5h / cx7d」の場所取り (幅 61 桁) で、Claude が届いても codex が届いても
    幅と行数は変わらない。1 回目は本物のキャッシュに codex の枠が無かったので codex の場所取りが cx7d の 1 行で、届くと 2 行に増えた (仕様どおりの限界)。
    R の盤も最初のコマから Claude / codex の段とカードの場所を取り、codex の段の見出しの行は届く前後で同じ
  - 残り: なし (P3 の 3 件は上の trigger 待ち)
