# 760 (research): tuikit の監査 (21 タイプ。2026-10-14。直接実行)

起票日: 2026-10-14

## 概要

`/audit` を直接実行で `src/tuikit/` (独立 module) と使う側の呼び出しに回した。タイプは ux を除く 21 種
(設計 7・品質 8・テスト 2・dependency・ci・lint-from-done・general)。前回 (10/09。702) 以降の 8 commit・+835 / −182 行を重点にした。
read-only の調査体 4 体を 1 体ずつ直列に回した (設計 7 種 = opus / 品質 8 種 = opus / テスト 2 種 = sonnet / その他 4 種 = sonnet)。
main は主要な主張を実物で確かめた: 756 の幅の崩れ (40/40/40/40 → 74/42/78/77) を自分で測った・widthenv を呼ぶ使う側が 3 つだけ・restartable の confirm の利用と固定版・
foreground の Test 関数が 2 本だけ・schedkeys の剰余算 5 か所・README の記述と workflow の paths。

## 結論 (起票先)

| 起票先 | 中身 | 今壊れているか |
|---|---|---|
| 756 | restartable が幅の環境の警告を実行時に出さない (P3。README に既知の制約として記載済み) | 枠は崩れる (main が実測) が、制約として文書化済み |
| 757 | 使う側に残った写しと暗黙の約束 5 項 (Exec の結果・lint の欠け・剰余算と移動キー・gutter の決め打ち・ESC の手書き走査) | 潜在 |
| 758 | foreground.Exec の SetStdin が未検査 (P3。他は pro-con が守る)・何も検査しないテスト 2 本・pty テストの Skip が見えない・任意 3 | テストの穴 |
| 759 | restartable の固定版・termwidth の不正な入力の契約・README | 潜在 / 文書 |

全数勘定: 採用 18 項目 = 756 が 1・757 が 5・758 が 8 (1〜4・4 のついでの時間の上限・5 の任意 3)・759 が 4 (1・2 の 2 件・3)。
pty テストの Skip が見えない件は品質の体と ci の体の両方が挙げたので 1 件に統合した。ci の発見はこの 1 件だけで、lint-from-done は 0

## 却下した指摘と理由 (再提起の前に読む)

702 の却下はそのまま有効。今回の分は 757・758 の却下節のほか:

- `WindowOffset` が rows≤0 を 1 へ寄せない: 呼び出し 9 か所はどれも `min(...)` で窓を切ってから描く
- `lineedit` の LRM / RLM (U+200E/F) が通る: 700 で設計として決めた範囲
- `Reclaim` の子の `c.Run()` に時間の上限が無い (K3): 子は ioctl 1 回で抜け、止まる経路を示せない
- gutter の桁あふれ: 表示する番号は桁数の元の行数を超えない (glogx・treefiler とも)
- 依存の版のずれ: 7 module で一致 (x/ansi v0.11.7 など)。go 版の表記の違い (`.github/go-version` 1.26.x と go.mod の 1.25.0) は CI のコメントどおり意図的
- termsafe の固定版と HEAD の差は文書だけ

## 攻めたが見つからなかった範囲

- race: `go test -race -count=1 ./...` 16 パッケージとも ok。package 変数は読み取り専用の表と `hlEscCache` (sync.Map) だけ
- dead-code: tuikit の `make lint` (unused・staticcheck 有効) 0 issues
- 速度 (実測。M 系 Mac): StripSGR 旧 57ms → 新 41ms・DropColumns 旧 63ms → 新 70ms (色付き 1,200 B の行 × 2 万回)。終端の無い OSC / DCS も線形
- 10/09 以降の寄せの写し残り: foreground の呼び出し・`sgr.XtermRGB` (表の値の写し 0)・`WindowOffset`・`lineedit.Acceptable` (文字を手で溜める入力欄 0)
- ci: paths の漏れ 0 (tuikit を含む workflow は src_tuikit を入れて 6 本)・キャッシュは setup-go と golangci-lint とも hit・run の 6 分の大半は macOS runner の待ち
- lint-from-done: `issues/done` の tuikit への言及 39 件を見て、既に lint / test で止めていないものは無し
- 範囲外のぼやき (未精査): `termsafe.DetailLine("\x1bあY")` が `"Y"` を返し、孤立した ESC が次の 1 文字まで落とす

## 進捗

- [x] 監査を実行し、発見を 756〜759 に起こした
- [x] 起こした issue の反証レビュー (read-only のサブエージェント 1 体。codex は使っていない)。取り下げ 0 件。訂正を別 commit で反映:
  756 を P3 へ (restartable の README と issue 588 が既知の制約として記録済み)・対象を restartable に絞る (schedkeys は曖昧幅の字 0・ratelimit は hook 経路の CLI)・
  pro-con は画面の中の通知・版上げ不要 / 757-2 に pro-con の ruleguard 不在と ExecProcess のコメント 3 か所 / 757-3 に MotionOf の意味の差 / 757-4 は doc と TestFormat の契約 /
  758-1 を P3 へ (pro-con のテストが変異 1・2 を検出。穴は SetStdin だけ)・treefiler の pty テストは ptytest を使わない (誤りを削除)・母集合の数え直し
- [x] 756〜759 を対応して done へ移した (2026-10-14)。残った確認の案内の不揃いは 761
