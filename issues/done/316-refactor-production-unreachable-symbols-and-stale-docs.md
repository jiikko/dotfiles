# refactor: production から到達しないシンボルと、そこから生じた嘘の doc を片付ける

起票日: 2026-09-07
カテゴリ: refactor
優先度: 中（個別の実害は小さいが、**doc が実体と食い違っている**ものが混ざっており、
読んだ人が誤った前提を持つ）
出典: /audit dead-code 2026-09-06。各行の production / test 参照数は私が機械で数え直した

🚨 **一括削除の issue ではない**。件ごとに「配線漏れ / 将来用 / 正当な seam」の判定が違う。
下の表の「対応」列を 1 件ずつ判断すること。

## 🚨 着手前に必ず読むこと（削除は「コンパイラが全 callsite を指す」では守れない）

監査の申し送りに「Go の field/func 削除はコンパイラが全 callsite を指すので取りこぼしは
構造的に起きない」とあったが、**誤り**。コンパイルを通したまま意味が変わる経路が
この repo に実在する:

1. **`==` で比較される構造体**: `ratelimitCacheKey` は `ratelimit_dashboard.go` で `==` されるので、
   フィールドを 1 つ落とすと**コンパイルは通ったままキャッシュキーの同一性が変わる**
2. **位置指定の複合リテラル**: フィールドの増減で**別のフィールドへ値が入る**
3. **reflect / encoding タグ経由**: `overlay_ownership_test.go` は reflect でフィールドを
   数え上げており**到達解析の外**

削除前に `==` / 位置指定リテラル / reflect の 3 つに当たるか grep すること。

## 一覧（production 参照はすべて実測）

| 場所 | 状態 | 対応 |
|---|---|---|
| `src/glogx/issues_view.go:anchorCursor` | production **0** / テスト **1**（ユーザー操作の表に混ざっている） | **判定が要る**。下の専用節 |
| `src/glogx/status_view.go:statusViewport.width` | 読み手が production にもテストにも無く、doc の「何桁 × 何行か」が嘘 | 消すか、doc を実体に合わせる |
| `src/doctor/disk/scan.go:guards.opt` | 構築時に詰めるだけで、パッケージ内のどのメソッドも読まない | 同上 |
| `src/glogx/tui.go:ciPollResultMsg.targets` | 構築時に必ず詰まるが、受け側ハンドラが一度も読まない | 同上 |
| `src/glogx/usage/render.go:RenderLine` / `RenderTable` | production **0**（`RenderTableGroups` / `RenderDashboard` は各 2 件で生きている） | issue 317 と併せて判断 |
| `src/doctor/disk/delete.go:DeleteReport.HasFailures` | production **0** / テストのみ。doc が「`err == nil` だけを見る呼び出し元が誤読するのを防ぐ口」と目的まで書いている | **想定消費者（glogx）が通っていない**＝配線漏れの可能性。優先 |
| `src/glogx/usage/usage.go:Window.Raw` | write-only。**サニタイズ対象外の untrusted 文字列が永続キャッシュに載り続ける** | issue 317 と同じ commit で |
| `scripts/tmux_resurrect_save.sh:tt_capture_contents_on` | 定義 1 行のみ、呼び出し **0**（born dead）。同じ判定式が repo 内 2 箇所にインライン重複 | 下の専用節 |
| `tests/zshrc/ai-commands/test_ai_commands.sh:assert_function_exists` | 定義のみ、呼び出し 0（厳しい版 `assert_is_function` に置き換わった残骸） | 削除 |
| `src/glogx/zoom.go:appZoom.start` | doc が**既に存在しない呼び出し元**（「Init から」）を名指ししている | doc を直す |
| `src/glogx/ime_tis_stub.go` | `//go:build !darwin \|\| !cgo`。**repo は macOS 専用**なので、どの lint / test / CI lane もコンパイルしない | 消すか、コンパイルする lane を作るか決める |
| `bin/sync_ratelimit_calendar.sh` | 必須の `ratelimit_resets.yaml` が **commit 80c1a5ad で削除済み**。新品チェックアウトでは起動即エラー | 削除（死んだエントリポイント） |
| `src/doctor` / `src/glogx/issues` の exported 8 件 | パッケージ外に消費者が 0（公開面が消費者より広い） | unexport できるものを絞る。**急がない** |

## `anchorCursor` の判定（ここだけ方針が割れている）

`commit e1154f30`（2026-09-05）が `clearNumberFilter` の呼び出しを `anchorCursorInternal` へ
差し替えた結果、production 参照が消えた。テストは `issues_group_view_test.go:277` の
**ユーザー操作の表**に 1 行として混ざっている。

**まず「配線漏れか将来用 API か」を判定する**。判定材料として、配線漏れだった場合の
ユーザー可視の誤動作を書いておく（これを書かないと「どちらでもよさそう」で放置される）:

> move の再アンカー予約が、**ユーザーが明示的にカーソルを置いた後も生き残って想定外の位置へ飛ぶ**

- **(a) 配線漏れなら**: 明示的にカーソルを置く経路（URL ピッカー確定 / 番号絞り込み確定）を
  `docs/issues-viewer-spec.md` と突き合わせて正しい呼び出し元へ差し替える
- **(b) 不要なら**: wrapper だけを削除する。🚨 **`anchorCursorInternal` を `anchorCursor` へ
  改名して畳む案は採らない** — 鏡像の `anchorGroup` / `anchorGroupInternal`
  （`anchorGroup` は `issues_view.go:1269` から production 到達可能）を壊し、
  cursor と group で命名規約が食い違う
- どちらでも `issues_view.go:1310` のコメント（「予約は捨てない（`anchorCursor` は捨てる側）」）の
  **宙吊り参照**を直す
- テスト表の該当行も同じ commit で外す

## `tt_capture_contents_on` の判定（方針が割れている）

全数勘定は済んでいる（repo 全文で出現は定義 1 行のみ / `TT_SAVE_SOURCE_ONLY` のテスト経路からも
呼ばれない / eval・変数経由の間接呼び出しも無し）。マスクしていた failure mode も確認済み
（「capture-contents が on なのに archive が無い」は `tmux_snapshot_health.sh` が別に検出しており、
消しても無防備になる経路は無い）。

- **単純削除**、または
- **`scripts/lib/tmux_resurrect_guards.sh` へ寄せて**、`tmux_snapshot_health.sh:114` /
  `tmux_restore_runner.sh:96` のインライン 2 箇所を呼び出しへ統一する
  （判定式が 3 箇所 → 1 箇所になる。`guards.sh` は両スクリプトが無条件 source 済みなので
  silent skip の窓は開かない）

寄せる側を選ぶなら、**述語を常に false へ倒す変異で 2 スクリプトのテストが red になるまで**確認する。
🚨 削除する側を選ぶ場合、**削除跡に説明コメントを足さない**（無くなったものの説明はノイズ）。

## 対応 (2026-09-09) — 全 13 行に判断を付けた

| 場所 | 判断 | 根拠 |
|---|---|---|
| `issues_view.go:anchorCursor` | **削除** | (b) 不要。予約を捨てる責務は `setCursor` / `g`・`home` / `moveTab` が `clearPendingMoveAnchors()` を直接呼ぶ形で**既に配線されている**（6 箇所）。`clearNumberFilter` が Internal を呼ぶのも意図的（Esc は「1 段戻る」でカーソル移動の意思表示ではない、と 1310 のコメントが明記）。鏡像の `anchorGroup` は production 到達可能なので残す |
| `status_view.go:statusViewport.width` | **削除**（🚨 下記のとおり最初の判断が誤りだった） | 私は「`o.width` が読んでいる」と書いたが、それは **`statusRenderOpts.width`** で**別の struct**。`statusViewport.width` の読み手は **0 件**（`vp.width` の唯一の読み手 `issues_view.go:1575` は `issuesViewport`）。敵対レビューが型の取り違えを指摘 |
| `disk/scan.go:guards.opt` | **削除** | 参照 0（`g.opt` が 1 件も無い）。`scanEntry` は `opt` を別引数で受ける。`guards{opt: opt}` はフィールド名指定で `==` / reflect も無し |
| `tui.go:ciPollResultMsg.targets` | **削除** | 受け側が読まない。一括取得（`ciResultMsg`）は「レスポンスに現れなかった target も loading 解除」に使うが、**こちらは fetching / detailsLoading を立てない**ので解除対象が無い = 構造的に不要。テスト 3 箇所も同時に修正 |
| `usage/render.go:RenderLine` / `RenderTable` | **残す（理由をコードに）** | production 0 は再確認した。`RenderTable` は `RenderTableGroups` の薄い包み（3 行）で、テストが平坦化の一致を固定している。消すとテストごと失われる（`refuse-low-value-coverage.md`）。**このクラスを機械で検出する話は issue 315** が持つ |
| `disk/delete.go:DeleteReport.HasFailures` | **配線した**（2026-09-08） | 配線漏れだった。結果画面の合計行が「解放しました: N」しか語らないので、その直後に警告行を出す |
| `usage/usage.go:Window.Raw` | **削除** | write-only。`usage_cache.go` の termsafe は `Label` しか通さないので、**サニタイズ対象外の untrusted 文字列が永続キャッシュに載り続ける**形だった。既存キャッシュに `"Raw"` が残っても Go の JSON は未知フィールドを無視するので読み込みは壊れない |
| `tmux_resurrect_save.sh:tt_capture_contents_on` | **guards.sh へ寄せた**（削除ではなく統合） | 判定式が 3 箇所（born dead な定義 1 + インライン 2）で完全一致していた。`guards.sh` は 3 スクリプトとも**無条件 source 済み**なので silent skip の窓は開かない。判定式は **3 → 1 箇所** |
| `test_ai_commands.sh:assert_function_exists` | **削除** | 定義のみ・呼び出し 0（厳しい版 `assert_is_function` に置き換わった残骸） |
| `zoom.go:appZoom.start` | **doc を直した** | 「(Init から)」は嘘。`tui.go:476` が `// m.zoom.start(timeNow())` とコメントアウトしており、その理由（起動が appZoomDuration 待たされる）もそこに書いてある。**対の `startClose` は生きている**ので関数自体は残す |
| `ime_tis_stub.go` | **残す（理由をコードに）** | 消せない。相方が `//go:build darwin && cgo` なので **`CGO_ENABLED=0` では黙って除外され**、stub が無いと未定義シンボルになる（`_go-project.yml` が `CGO_ENABLED: "1"` を明示しているのはこの罠のため）。**`CGO_ENABLED=0 go build ./...` が通ることを実測して確認**。どの lane もコンパイルしないので中身の退行は誰も検出しない、をコメントに明記 |
| `bin/sync_ratelimit_calendar.sh` | **削除** | 必須の `ratelimit_resets.yaml` が commit 80c1a5ad で削除済み。tracked な参照は issue 本文だけ |
| `doctor` / `glogx/issues` の exported 8 件 | **残す（急がない）** | issue 自身が「急がない」と分類。unexport は消費者の確定が要り、315 の仕組みが入ってから判断する方が安い |

### 🚨 敵対レビュー（opus）で自分の判断が 1 件ひっくり返った

**`statusViewport.width` は「残す」ではなく「削除」が正しかった。** 私は
`status_view.go:1031` の `o.width` を読み手として挙げたが、そこの `o` は
`lines(o statusRenderOpts)` の引数で**別の struct**。`statusViewport` を受ける 5 関数
（`handleKey` / `pagerKeyPress` / `listKey` / `openPager` / `openNeighborPager`）が読むのは
`vp.page` と `vp.colored` だけで、**`vp.width` の唯一の読み手は `issuesViewport` の方**
（`issues_view.go:1575`）だった。型名が似ていて紛らわしい。

放置すると実行時には壊れないが、**`done/` に「issue の主張が誤り」という誤った結論が残り、
次の監査（315 / 318）がその上で棄却する**。削除して doc も「何桁 × 何行か」→「何行か」へ直した。

### レビューで直したその他（全件）

- **P2**: 新テストの negative control が**到達を証明していなかった**。archive チェックの手前には
  早期 return が 4 本ある（既定サーバでない / 先任が実行中 / lock 失敗 / restore.sh 未解決）ので、
  どれかで抜けた run が「記録しなかった = 正しい」に化ける。→ `assert_reached_archive_check`
  （`restore-manual-begin` の存在）を全ケースの前に置いた
- **P2**: `! gzip -t` 節と `[ -f ]` 節を**1 mm も守っていなかった**（壊れた archive 1 種類しか
  fixture が無かった）。→ 「健全な archive」「archive が無い」の 2 ケースを追加
- **P2**: `anchorCursor` の宙吊り参照が `issues_view.go:300` に**もう 1 件残っていた**
  （1310 だけ直して満足していた。この issue の主題そのものの再生産）
- **P3**: `RenderTable` の新コメントが 8 行上の既存 doc（「単独コマンド案を捨てるならテストを
  寄せてから削除してよい」）と矛盾していた。平坦化の一致を固定しているのは
  `TestRenderTableGroups` の **1 本だけ**なので「消すとテストごと失われる」は誇張。両立する形へ
- **P3**: `grep -qE "…path=$RDIR/…"` が `$RDIR`（`mktemp -d` 由来で `.` を含む）を未エスケープで
  ERE に埋めていた。→ パスは `grep -qF`、epoch の数字は別行の `-qE` で見る
- **P3**: `guards.sh` の新コメントが「on のときだけ報告する契約」と書いていたが、
  `tmux_resurrect_save.sh:tt_archive_finalize` は**この述語のゲート外**（`[ -f ]` で暗黙に絞る）。
  利用者は 2 本だけ、と実体に合わせた

### レビューが「壊せなかった」と明記した観点

`Window.Raw` の削除（`DisallowUnknownFields` は repo に 0 件で旧キャッシュは無視される） /
`guards.opt` の削除 / `ciPollResultMsg.targets` の削除（受け側は `gen`/`batch`/`ghErr` しか触らず、
ciPoll が `fetching`/`detailsLoading` を立てる経路も無い） / `ime_tis_stub.go` を残す判断
（`CGO_ENABLED: "1"` は workflow レベルの env で 11 workflow に継承され、上書きも無い） /
`appZoom.start` の doc 修正 / `tt_capture_contents_on` の統合による silent skip の窓
（source 失敗時も直後の `tt_on_default_server || exit 0` で fail-closed）。

### 未対応として記録するもの（この issue のスコープ外）

- **`numberFilterKey` の default 分岐（`issues_view.go:1512`）が、カーソルを置くのに
  `clearPendingMoveAnchors()` を呼ばない唯一の production 経路**。移動キーで issue を動かした
  直後に数字を打つと予約が生き残り、scan 着弾時にフィルタ先頭ではなく移動先へ飛ぶ。
  **本 commit の退行ではない**（`anchorCursor` はここからも呼ばれていなかった）が、
  削除の根拠として書いた「配線の全数勘定」が不完全だったので記録する
- **`test_snapshot_health.sh` は「off でノイズを出さない」を検査していない**（true 変異で緑）

### 削除前の 3 経路チェック（受け入れ条件）

`==` 比較 / 位置指定リテラル / reflect のいずれにも当たらないことを削除対象ごとに確認した:

- `guards.opt`: 構築は `&guards{opt: opt}` の**フィールド名指定** 1 箇所。`==` / reflect ともヒット 0
- `ciPollResultMsg.targets`: 構築は全部フィールド名指定（production 1 + テスト 3）。`==` / reflect ヒット 0
- `Window.Raw`: `Window{...}` はすべてフィールド名指定。`==` / reflect ヒット 0。テストの読み手 1 箇所を修正
- `anchorCursor` / `assert_function_exists` / `sync_ratelimit_calendar.sh`: 関数・ファイルなので 3 経路は非該当

### 変異検証

**`tt_capture_contents_on` を guards.sh へ寄せた件**（issue が「述語を常に false へ倒す変異で
2 スクリプトのテストが red になるまで確認する」と要求していた分）:

| 変異 | `test_snapshot_health.sh` | `test_restore_runner.sh` |
|---|---|---|
| 述語を常に **false** | **red** ✅ | **red** ✅ |
| 述語を常に **true** | green（下記） | **red** ✅（negative control） |

🚨 **確認しようとして穴が 1 つ出た**: `tmux_restore_runner.sh` の archive 完全性チェック
(`restore-archive-broken` の記録) に**テストが 1 本も無かった**。upstream は展開失敗を検証せず
rc=0 で完走するので、この記録が無いと「window は全部戻ったのに全 pane の scrollback が空」が
完全に silent になる経路そのもの。→ positive（on で記録する）と negative（off では記録しない）を
両方足した。

🚨 **true 変異で `test_snapshot_health.sh` が green** なのは、そちらが「off のときに
ノイズを出さない」を検査していないため。false 変異では red なので issue の要求は満たすが、
**off 側は無検査**であることを記録しておく（新しく見つけた穴。この issue のスコープ外）。

🚨 **測定を 1 回間違えた**: 変異ループを
`bash "$t" > out 2>&1; printf '%s rc=%s\n' "$(basename $t)" "$?"` と書いたため、
**`$?` が `basename` の rc になり両変異とも「green」に見えた**。手で当て直して red を確認し、
`rc=$?` を先に退避する形へ直した（`measure-external-cli-streams-separately.md` の
「rc は同じ行のコマンド置換で壊れる。先に変数へ退避してから使う」そのもの）。

## 受け入れ条件

- [x] 表の各行に「消した / 配線した / 残す（理由）」のいずれかが付き、**残す判断はコード直近に
      理由をコメントで残した**（`RenderLine` / `RenderTable` / `ime_tis_stub.go` / `appZoom.start` /
      `ciPollResultMsg` / `Window.Raw` / `tt_capture_contents_on`）
- [x] 削除するものは `==` / 位置指定リテラル / reflect の 3 経路を grep した結果を上に書いた
- [x] `make test` が緑（既知の失敗を除く）

## 関連

- issue 315（このクラスを CI が構造的に検出できない理由。**個別に潰しても再発する**）
- issue 317（`usage` パッケージの公開面と termsafe の射程。`RenderLine` / `Window.Raw` は両方に出る）
