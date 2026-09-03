# 242 research: doctor の ux / ui-components / test-helpers / issues-done / general 監査 (2026-09-04) の全数勘定

起票日: 2026-09-04
種別: research（監査記録。レポート本体は `tmp/` = gitignore なので結論だけをここへ移す）
出典: audit `general` / `issues-done` / `ux` / `ui-components` / `test-helpers`（直列実行。ux/ui-components のみ
サブエージェント 1 体、他は main が実施）
範囲: `src/doctor/**` と `src/glogx/doctor_*.go`（+ 比較対象として glogx の他ビューア）

🚨 **leaky-abstraction は回していない**。同日 23:44 に別セッターが forge-Minimum で実施済みで、
却下台帳が [237](237-research-leaky-abstraction-audit-2026-09-03.md) に在る（再実行すると却下済みの
指摘を再生成する）。削除経路の security / error-handling / broken-code / design も
[236](236-research-doctor-delete-audit-2026-09-04.md) が同日 02:11 に実施済み。

## 全数勘定

| 区分 | 件数 | 行き先 |
|---|---|---|
| 起票した（裏取り済み） | 4 | [238](238-bug-doctor-disk-row-width-budget-ignores-gutter.md) P2 / [239](239-bug-doctor-paths-truncated-from-the-right.md) P2 / [240](240-bug-doctor-inspect-entries-cannot-be-selected-per-directory.md) P2 / [241](241-bug-doctor-confirm-panel-has-no-scroll-and-swallows-keys.md) P2 |
| P3 として本 issue に記録 | 5 | 下記 |
| 却下（攻めたが 0 件 / 共通化しても複雑性が下がらない） | 8 領域 | 下記 |
| 未確認リスク（発火条件を示せなかった） | 3 | 下記 |

## P3（単独では起票しない。触るときに一緒に直す）

1. **確認画面が `Failed` を「🚫 対象外」と呼ぶ** — `doctor_delete.go:confirmLines` は
   `skipped := OutcomeSkipped || OutcomeFailed` で 2 つを 1 語に畳む。`disk` の定義では
   Skipped =「触らなかった」/ Failed =「実行できなかった」で別物、`doctorOutcomeWord` は結果画面で
   `🚫 触れず` / `❌ できず` と分けている。しかも `🚫 対象外` は一覧（`disk.Mark`）では
   `StatusBlocked` の語。**doctor 内で 1 箇所だけ規律から外れている**。到達しやすいのは
   「削除の前に走査し直せませんでした」。`doctor_delete_test.go` は Skipped の組み合わせしか pin していない。
   直し方: `doctorOutcomeWord(e.Outcome)` を使う（語彙は既にある）。
   🚨 P3 に置いた根拠は**倒れる向きが安全側**（触らなかった側に見える）であること。
   同じ画面の 241 / 236 が P2 なので、「読み違えて y を押す」経路が示せたら上げる
2. **削除エラーのパネル本文と hint が同時に違うことを言う** — パネルは
   `y: 出力をコピー   他のキー: 閉じる`（`doctor_delete.go:549`）、hint は
   `…閉じてもう一度スキャン`（`doctor_view.go:517`）。実挙動は `doctorRescan` を返すので**hint が正しい**。
   兄弟の結果パネルは正しく言っており、エラー経路だけ取り残されている
3. **下見（DryRun）中の hint が「実行中です」** — `blocking() = preparing || running` なので、
   まだ何も壊していない時間帯に「実行中」と読ませる（パネルは「確認しています」と言う）。
   さらに `Ctrl-C を 2 回押すと中断します` は running 側にしか無く、**下見中はパネルに抜ける手段が無い**
   （機能上は効く）。直し方: hint を preparing / running で分け、中断の案内はフェーズに依らず出す
4. **doctor の `g` / `G` だけ `home` / `end` を受けない** — `status_view.go` / `issues_view.go` は
   `case "g", "home":` / `case "G", "end":`。doctor だけ別名が無い（`ctrl+d` / `pgdown` は両方受ける）
5. **`rowCursor.restore` の「行が 0 件」分岐だけがキーを捨てる** — 同ファイルの `remember` は
   「選べる行が無いフレームでは既存の key を保持する（捨てると index 保持へ退行する）」と明記しているのに、
   `len(rows) == 0` の分岐は `c.key = ""` にする。`buildRows` が必ず区切り行を積むので
   **production では到達不能**、0 行フレームを固定するテストも無い（236 の P3-5「`trashMove` の
   `dev==0 && ino==0` 検査は到達不能」と同じクラス）

## 却下（再提案しないこと）

- **`doctor_rowcursor.go` 全体** — key 保持・寄せ・`fellBack` の報告・窓寄せの不変条件を読み切り、
  退行を作れる入力を構成できなかった。**doctor は 3 ビューアで唯一「カーソルが寄った」ことを通知する**
- **`doctor_brew.go:parseBrewDoctor`** — 前置き除去 / 空行の畳み / rc=0 の Warning 抽出 /
  rc≠0 で警告 0 件を「診断できず」へ倒す経路。false green を作れなかった
- **`doctor_cleanup.go` / `cleanup_latch.go`** — `doctorTrack` が `f` の前に `add` する順、
  `waitDoctorCleanup` の 200ms 通知、`done()` が n を負にしない形（issue 217 で敵対レビュー済み）
- **トーストが全画面 doctor の上に出るか** — `finishWithGlobalChrome` が必ず重ねる。見えない経路なし
- **`updateKeyReachable` と削除の相の取り合い** — `ownsKeys() = del.active()` で `C`/`X` に譲る。
  確認中・実行中に update が始まる経路を作れなかった
- **`docs/theme-colors.md` との整合** — 同文書の適用範囲は tmux / nvim。glogx への言及は
  `render.go:ansiFrameBorder` の 1 行のみで、doctor の色は対象外（この文書で doctor を測るのは筋違い）
- **ui-components の共通化余地** — 「パネルの末尾を優先して残す」機構を要るのは doctor の削除パネルだけで、
  他の overlay（usage / job / diff / prStatus / actionModal）は小さい box か 1 行確認。共通化で複雑性は下がらない。
  **唯一の共通化提案は 238 の gutter 定数**
- **test-helpers** — `&doctorView{}` の直接構築 32 箇所（`doctor_*_test.go` 31 + `hint_width_test.go` 1）と
  `doctorTestView(t)` 35 箇所の併存は**正当**
  （前者は純関数テスト用の最小構築、後者は一時 HOME・実ファイル・fake runner を作る重い fixture。
  軽い方を重い方へ寄せると遅くなり結合も増える）。ローカルの `res := func(...)` 2 件はシグネチャが別物

## issues-done（移動候補 0 件。ただし本文のドリフト 2 件）

- **[227](227-bug-glogx-fullscreen-viewer-registry.md) の「応急」は既に landed**（`gitlog_watch.go:gitLogReloadDeferred` に
  `m.doctorOv.visible()` が在る。commit `c973646e`）のに、本文は未対応のまま書かれている。
  残るのは構造（全画面サーフェスの単一の出典）だけ
- **issue 222 の本文が `fdf51bb2` を反映していなかった** → 2026-09-04 に残件ごと片付けて done へ送った
- issue 219 は別セッターが実装済みで done/ にある（`9d43a707` / `41769a28`）
- その他の open issue（220 / 223 / 224 / 226 / 228-235、pending 4 件）に fix commit は無い

## 未確認リスク（発火条件を示せなかった。観測ポイントとして残す）

1. **`fitHintItems` は `prio` が 1..7 の外の項目を幅に関係なく無言で落とす** — 今日の呼び出しは全部
   1..7 に収まっており実害 0 件。次に「一番落としてよい項目」を足す人が 8 を書くと静かに消える
   （テストは `assertFits` = 幅超過しか見ない）。**発火は「次の編集」であって現在の入力ではない**
2. **issues / status viewer のカーソルも再構築で無言に別の行へ移る**（doctor の issue 210 と同型）。
   外部編集で再スキャンが起きる経路が想定発火点だが、実行時の再現は作っていない
3. **doctor には `docs/` の spec が無い** — `docs/README.md` の「仕様 (契約。実装より仕様が先)」には
   issues viewer と status viewer の 2 本しか載っていない（「write する画面」の注記は status viewer の
   行だけで、枠の定義ではない）。**実際にファイルを削除する唯一の画面**である doctor の契約は
   `issues/done/148` に埋まっている（done = 時点の記録）

## 測り方の注記（次の監査の起点）

- `fitHintItems` の doc「今の利用者は status viewer だけ」「2 つ目の利用者が出た時点で共通の場所へ移す」は
  **現在 4 呼び出し（status / doctor / detailOv / job パネル）で事実として古い**。移動は推奨しないが記述が実態と違う
- test fixture の `disk.Entry` が production の不変条件（`DeleteVia` 等）を満たしているかは**未測定**。
  正規表現で数えると入れ子の `{}` を含むリテラルを取りこぼす偏りが出る（go/ast で測ること）
