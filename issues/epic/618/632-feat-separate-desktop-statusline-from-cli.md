# 632 (feat): ステータスバーを CLI と desktop で分け、desktop 向けに最適化する

起票日: 2026-10-03

epic [618](pending/618-design-claude-code-mods-migration.md) の子。[625](pending/625-feat-mods-statusline-on-desktop.md) (desktop にステータスバーを出す) の続き。

## 概要

desktop の Code タブのステータスバー (mod `desktop-statusline`) は、CLI の `_claude/statusline-command.sh` の出力 (ANSI の色つきの文字列) を、
desktop の部品へ機械的に置き換えて描いている。desktop で見ると**微妙に崩れていて、色が背景と合っていない** (2026-10-03、ユーザーの報告)。
CLI と desktop で描き方を分け、desktop は desktop の部品とテーマに合わせて描く。

## 今の形 (2026-10-03)

- 表示の出典は script 1 本 (652 行の bash)。項目の計算・色の閾値・文言・並べ方・色付け (ANSI の SGR) がこの 1 本に混ざっている
- desktop は mod (`_claude/mods/desktop-statusline`) が同じ script を呼び、`hooks/ansi.ts` で ANSI を desktop の `Text` の色に置き換える。
  変換先のパレット (`FG` / `BG`) は**暗い背景向けの固定の hex** (例: `37` → `#cdd9e5`、`30` → `#2d333b`) で、desktop のテーマ (ライト / ダーク) と連動しない。
  CLI は端末自身のパレットで描かれるので、この問題は desktop にだけある
- script が使う SGR は基本の 16 色だけ (256 色・truecolor は 0 件。2026-10-03 に grep)。文字色 `30`〜`33` / `35`〜`37` / `90` / `94` (`34` は不使用)、背景 `41` / `42` (`42;30m` = 緑の背景に黒の文字 など)、太字 `1`・下線 `4`・点滅 `5`
- 型定義 (plugin-authoring skill の `claude-code.d.ts` の `TextProps`) では、desktop の `Text` の色は「テーマのキーか、生の色」を受ける。
  テーマのキーを使えばテーマに追従しうる (キーの一覧と、desktop で実際に追従するかは未確認)
- **崩れの中身は未特定** (スクリーンショット未取得)。候補: 5h / 7d の格子 (`5h [ 1 2 3 4 5 ] …%`) などが等幅フォントの空白揃えを前提にしている /
  背景色つきの区切りの余白 / 1 行目が desktop の幅で折り返す

## 期待する動作

- CLI の見た目は変えない
- desktop では、テーマの背景に馴染む色と、等幅に頼らないレイアウトで、CLI と同じ情報
  (ディレクトリ・ブランチ・セッション名・モデル・文脈の使用量・effort・advisor・5h / 7d の枠の消費ペース) が読める
- 🚨 advisor は今の desktop で出ていない: script は advisor を transcript から読む (`advisor_part`) が、mod の `statusInput` (`hooks/input.ts`) は `transcript_path` を空で渡す。
  描き方を分けても、この情報差は別に埋める必要がある (issue 625 の「`advisor` は script が settings から読むので影響なし」という記述は、script の注記と食い違う)
- ペースの状態と色の意味は CLI と同じ (上限 / 超過 / 先行 / 適正の緑・黄・赤に加え、余裕 = 明るい青 `94`、余剰 = マゼンタ `35`。役割の設計にはこれも含める)
- **CLI と desktop の間で、閾値と状態の判定を新たに分けない**。閾値と状態の判定は、すでに script と Go (`src/ratelimit/usage`、glogx が使う) の 2 実装で、
  `src/ratelimit/usage/pace_drift_test.go` の `TestPaceRulesMatchStatusline` (閾値・状態) と `TestPaceColorsMatchStatusline` (状態語と SGR の色) が一致を検査している。desktop で 3 つ目にしない

## 対応方針 (案。着手時に決める)

ユーザーの依頼は「CLI と desktop で分離」。どこまで共有するかは着手時に決めるが、次の順で考える:

1. **先に崩れの実物を見る** (スクリーンショットか、どの行がどう崩れるかの記述)。原因が色だけなら、アダプタ (`hooks/ansi.ts`) の変換先をテーマのキーにするだけで済む可能性がある
2. **データと描画を分ける**: script (かその計算部分) が「項目と意味上の役割」(`dir` / `branch` / `ok` / `warn` / `crit` …) を構造化して出す (`--json` 等)。
   CLI は従来どおり ANSI で描き、desktop の mod は役割をテーマのキーに対応させ、desktop の部品で並べる。項目と閾値の計算は 1 箇所に残る
3. 2 で足りなければ、desktop のレイアウト (並べ方・短縮・折り返し) を mod 側に完全に持たせる

- 見た目は本体に入れる前に見本で決める (`_claude/rules/decide-layout-in-sample-renderer-first.md`)。dev-mods で desktop に見本を出して候補から選ぶ
  (`docs/claude-mods-guide.md` の「試すだけ」)
- script を組み替えるなら、**CLI の出力が 1 文字も変わらないこと**を、組み替え前後の出力の比較で固定してから行う。
  script は stdin のほかに現在時刻・git の状態・セッション名・transcript を読み、実行時にキャッシュを書くので、比較はそれらを固定し、隔離した環境で行う

## 考えること

- desktop のテーマのキーの一覧と、`Text` の色にキーを渡したときの desktop での効き方 (型定義では受けるが未実測)。
  ライト / ダークを mod から知る手段 (`$.config.list()` の `theme` の行が使えるか)
- 構造化の出力を bash のまま足すか、計算を repo の Go へ移すか。移すなら CLI の statusLine の起動時間 (今 約 0.16 秒。2026-10-03 に 3 回計測) を悪化させない
- desktop の幅 (`ui.render` の props の `bodyColumns`) に応じた短縮の要否

## 受け入れ条件

- [ ] desktop での崩れの実物を記録した (何がどう崩れるか)
- [ ] desktop の色が、ユーザーが使うテーマの背景に馴染むことを人が確かめた
- [ ] CLI の出力が変わっていないことを機械で確かめた (変更前後の出力の比較)
- [ ] CLI と desktop の間で項目・閾値・状態の判定を新たに分けていない (既存の script / Go の 2 実装と drift テストはそのまま)
- [ ] desktop でも advisor が出る (または出さないと決めて理由を書いた)
- [ ] mod のテストと変異検証、`docs/claude-mods-guide.md` の更新

## 関連ファイル

- `_claude/statusline-command.sh`
- `_claude/mods/desktop-statusline/hooks/register.tsx` / `ansi.ts` / `input.ts`
- `src/ratelimit/usage/pace_drift_test.go`
- `docs/claude-mods-guide.md` (desktop-statusline の仕組み)

## 進捗

- 2026-10-03: 起票 (ユーザーの依頼)
- 2026-10-03: codex の反証レビュー (P2 4 件・P3 1 件、すべて実コードで裏取りして反映): 閾値のテスト名の誤り / 余裕・余剰の色の欠落 / advisor が desktop で出ていない / 出力比較の固定条件 / 不使用の `34`
