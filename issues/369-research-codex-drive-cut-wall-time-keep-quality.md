# 369 research: codex-drive の実行時間と Claude 側の作業負荷を、出力の質を落とさずに下げる

- 起票: 2026-09-12
- 種別: `research` (skill の運用改善。結論が出た項目から `~/.claude/skills/codex-drive/SKILL.md` へ反映する)
- 出典: obaket の 3 epic (quicksearch-headless 747 / bandwidth-limit 650 / deterministic-test-time 736) を
  codex-drive で並行して回した 2026-09-03〜09-12 の実測と retro (obaket 725 / 763 / 765)
- **対象外**: usage limit による停止 (避けようがないので考慮しない。ユーザー指示 2026-09-12)

## 観測 (obaket、2026-08-30 → 09-12 の 13 日間)

| epic | commits | production | tests | issue/docs | 状態 |
|---|---|---|---|---|---|
| 747 quicksearch | 63 (4 日) | +4.4k | +7.3k | +2.7k | 完了 |
| 650 bandwidth | 70 (10 日) | +9.5k | +20.6k (+golden 8.4k) | +2.9k | 未リリース (gate false) |
| 736 deterministic | 100 (6 日) | +5.2k | +6.3k (+gate 2.6k) | +3.5k | 段階 4 作業中 |

- テスト行数が 108.7k → 162.2k (**+49%**)、production は +18%。`macOS/bin` に 13 日で 33 ファイル追加
- 650 の issue 本文は 1,917 行 (checkpoint を issue に積む運用)
- 時間を食っている箇所は retro に実測がある:
  - codex は macOS target の型検査ができず、**マイルストーンごとに 2〜3 往復** (763 項目 4。SwiftLint 違反 /
    `?? Never` / MainActor 隔離 / 捏造 API)
  - 敵対レビューが **2 ラウンド連続で 8〜9 件** (725)。ただし修正差分が新しいバグを作り 2 周目が拾った実例が
    2 件 (763 項目 1 / 765 項目 2) あるので、周回は削れない
  - codex の変異検証は sandbox で `swift test` が走らず typecheck だけで「成功」と書く (650 M1)。
    **Claude が全部当て直す**ので、codex に回させた分は丸ごと無駄
  - Claude 自身の全数勘定の誤り 2 件 / codex の変異結論の誤り 2 件 (734 M3)。D1.5 のクロス批評が拾ったが各 1 ラウンド
  - 敵対レビューの指摘に対し codex が **production に test 専用 seam** を足してくる (650 M1 fix3) → 差し戻しで 1 往復

🚨 **どの工程に壁時計が何分かかったかは記録が無い**。`codex-fanout` の `runs.tsv` は label / rc / out / log の
4 列で、所要時間を持たない。最適化の前にここを測る (`perf-claims-need-measurement.md`)。

## 案 (効きそうな順。各案の「質を落とさない根拠」を併記。codex 反証レビュー 2026-09-12 を反映済み)

### 1. 計測を先に入れる (前提)

- `codex-fanout` の `runs.tsv` に **開始時刻 / 所要秒** を足し、merger の行も台帳に載せる (今は merger は別起動で行が無い)。
  `codex-run` は fanout に委譲しているので同じ列が出る
- **これだけでは壁時計の内訳は測れない** (codex run 単位しか取れない。merger / Claude の digest 読み / build・test / 修正 /
  issue 更新は外)。マイルストーン単位で「開始・終了時刻 / codex 往復回数 / 型エラーによる差し戻し回数 / 敵対ラウンド数 /
  読んだ digest 行数」を checkpoint に 1 行で残し、run 合計と通しの壁時計を**別々に**書く (`perf-claims-need-measurement.md`)
- 質を落とさない根拠: 記録するだけ

### 2. 型エラー往復を減らす (最大の候補。763 項目 4)

- 現行 skill は「codex に xcodebuild を試みさせない / xcodebuild は Claude が素の環境で実行」と決めている (SKILL.md の `[2]`)。
  これは変えない。変えるのは 2 点:
  - shared SPM を触るマイルストーンでは、実装プロンプトに **「返す前に `cd shared && swift build --build-tests` を通す」を固定文で入れる**
    (sandbox で走るかは repo ごとに未確認 = 最初のマイルストーンで確かめてから固定する。SwiftLint plugin が同時に走るかも同様)
  - macOS target を触るマイルストーンは **薄く切り**、codex が返した直後に Claude が `[3]` の型検査 (`make build` 相当) を
    **レビュー fanout を起動する前に**回す。今はレビューと並走させて型エラーを後から知る形になりがち
- 質を落とさない根拠: 型エラーはどの往復でも最終的に直る。減るのは「型エラーを抱えたままレビューに出す」往復
- 却下した形: 「macOS の配線は Claude が書く」— skill の役割分担 (Claude は重い実装を書かない) と衝突する

### 3. `[3.8]` の実行ループを、sandbox で test が走らない repo では codex に回させない

- skill は既に「sandbox で実行できなければ Claude が全件やり直す」fallback を持つ。**その repo で走らないことが 1 度分かったら
  以降は最初から Claude のハーネスで当てる** (変異 patch の生成だけ codex read-only に作らせる)。obaket macOS はこの条件に当たる
- 質を落とさない根拠: 判定はどちらでも Claude の実行結果。減るのは走らない実行ループ 1 回 (max effort・15〜40 分)。
  Claude 側の作業は増える (明示的な例外として skill に書く)

### 4. 敵対レビューの指摘の「小修正」を Claude が直接当てる範囲を、明示の判断つきで広げる

- 現行例外は「確定的な 1〜2 行」。**「1 ファイル・20 行以内・設計判断を含まない (契約 / 不変条件 / 責務を変えない)」まで広げる**新規判断。
  行数は目安で、判定の軸は「設計判断の有無」。迷ったら codex に戻す
- 「r2 は r1 で新設したものだけを攻める」は `adversarial-review-own-safeguards.md` §7 に既にある (新案ではない。運用で守る)
- 質を落とさない根拠: 直した差分は §7 どおり r2 が攻める。減るのは codex への修正指示の作文と型エラー往復

### 5. checkpoint の「経緯」の量に上限を置く (必須項目は減らさない)

- 650 は 1,917 行。issue/docs が 3 epic で +9.1k 行 (全差分の約 12%)。Claude のトークンで書いている
- skill が checkpoint に必須とするもの (採用設計の要点 / M 表と commit hash / 手順 / 再開方法) と、ルールが要求する
  「全数勘定・却下した指摘と理由」は**減らさない**。上限を置くのは **「踏んだ罠」「経緯」「敵対レビューの原文引用」**の節で、
  各 M で 10 行以内にし、原文は tracked な `macOS/docs/` の設計 doc か digest の**要約**に置く (`tmp/` は消えるので参照先にしない)
- 質を落とさない根拠: 再開に要る情報と却下理由は全部残る。落ちるのは経緯の再説明

### 6. codex が書くテストの規約を要件ファイルの固定文にする (間接コスト。obaket issue 781 の実例)

- 同じ repo で 3 epic を並行させた結果、一方の epic が禁止した形 (実時間待ちの `pollUntil`) を他方の codex が量産した
  (TransferUploadBodyTests 0→53、QuickSearchSessionTests 0→28。gate の射程外)
- 案: `[R]` の要件テンプレに **「unit test では時間・スケジューリングを注入し、`pollUntil` / `Task.sleep` を新規に書かない。
  実時間そのものを検証する integration test は別に分けて明示する」**を固定文で入れる。fanout は共通 header を自動注入しないので、
  manifest の prompt 部品として毎回連結する
- 質を落とさない根拠: unit test の主張は変わらない。実時間が仕様のテストは分離して残す (注入で検証対象の意味が変わるのを避ける)

## 却下した案 (理由を残す。次に同じ案が再生成されないため)

- **`[3.5]` と `[3.6]` を 1 回の fanout に畳む**: `[3.6]` は `[3.5]` の修正後の green を前提にし、同時実行は skill が禁止している。
  lens を 1 本に減らす形は「視点の多様性」に反する。修正後に `[3]` を飛ばす形は検閲省略。**採らない**
- **production への test seam 禁止をプロンプトに足す**: 実装プロンプト・`[R]`・`[3]` の 3 箇所に既にある。新案ではない
- **effort / モデルを下げる**: 541 の実測 (low で「動くが筋が通っていない」実装) があり、ユーザー決定で max 固定
- **敵対レビューのラウンド数を固定で 1 にする**: 763 / 765 で 2 周目が実害を拾っている
- **lens 本数を減らす**: 「独立した視点」が質の源。減らすのは起動回数であって観点ではない

## 受け入れ条件

- [ ] 案 1 (計測) を入れ、1 マイルストーン分の内訳 (run 合計と通しの壁時計を別々に) を取る
- [ ] 内訳を見て案 2〜6 のうち効く順に SKILL.md へ反映する (反映した案ごとに前後の所要時間を checkpoint に残す)
- [ ] 質の比較は件数の増減で判定しない。**同じ変異セット**での red / green / hang の結果表と、r1 の P1 の**内容**を前後で並べ、
      「前は拾えていた種類の指摘が消えていない」ことを Claude が読んで確認する

## 関連

- `~/.claude/skills/codex-drive/SKILL.md` — 対象
- obaket `issues/done/725-retro-722-724-codex-drive-2026-09-06.md` / `763-retro-quicksearch-headless-step2-2026-09-09.md` /
  `765-retro-quicksearch-headless-step3-2026-09-10.md` — 実測の出典
- obaket issue 781 — 案 6 の具体例 (実時間待ちテストの量産)
- `_claude/rules/perf-claims-need-measurement.md` — 案 1 を先にやる根拠
- codex 反証レビュー (2026-09-12、read-only 1 本): P1 4 件 (案 5 旧版の同時実行と lens 削減 / 案 2 旧版の xcodebuild と役割分担 /
  案 7 旧版の再開情報の欠落 / 案 1 の測定範囲) と P2 5 件をすべて反映した。obaket 側の実測値は codex の参照範囲外で未反証 (出典は上記 retro)
