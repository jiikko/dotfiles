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

## 受け入れ条件

- [ ] 表の各行に「消した / 配線した / 残す（理由）」のいずれかが付き、**残す判断はコード直近に
      理由をコメントで残す**（[`pending-issue-rationale-in-code.md`](../_claude/rules/pending-issue-rationale-in-code.md)）
- [ ] 削除するものは、着手前に `==` / 位置指定リテラル / reflect の 3 経路を grep した結果を書く
- [ ] `make test` が緑（削除で壊れないこと）

## 関連

- issue 315（このクラスを CI が構造的に検出できない理由。**個別に潰しても再発する**）
- issue 317（`usage` パッケージの公開面と termsafe の射程。`RenderLine` / `Window.Raw` は両方に出る）
