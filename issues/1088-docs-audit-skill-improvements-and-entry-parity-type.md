# 1088 (docs): audit skill の改善点と、UI 層・debug API の経路の一致を見る観点の追加

起票日: 2026-11-18

## 概要

`_claude/skills/audit/SKILL.md` (v1.5.0・22 タイプ) を読み直し、直す点と足す観点をまとめる。
ユーザー指示 (2026-11-18) で、「debug API (UI を通さずにプログラムからアプリを操作する層) がある repo では、UI との差が限りなく小さく、
debug API が通れば動作確認が済む」「UI 層にビジネスロジックが無い」を見る観点を足すことも頼まれた。指示の妥当性を調べた結果を下の 2 節に書く。

## 1. 直す点 (今の SKILL.md と実態のずれ・抜け)

- [ ] **`ci` と `concurrency` の「まだ実走していない」が古い**。Step 2 の下の注記 2 つ。dotfiles の `issues/audit-log` には ci が 16 行、concurrency が 18 行ある
      (2026-11-18 に `awk -F'\t' '{print $2}' issues/audit-log | sort | uniq -c` で数えた)。直近の 1070 (src/slack-bbs の全観点の監査) では
      ci 0 件 (CI の paths の追加あり)・concurrency 0 件 (記録 1) だった。注記を「初回に見ると書いた C1〜C4 / K1〜K4 の分類が使えたか・偽陽性が出たか」の結果に置き換える。
      結果を数えるには audit-log の出力ファイル (issue) を読む必要があり、ここではまだ読んでいない
- [ ] **error-handling の指針が `~/.claude/rules/warn-log-on-unexpected-behavior.md` (2026-10-30) を見ていない**。今の指針は「握りつぶされたエラー」を探すが、
      best-effort で続けてよい失敗に warn のログが出ているかを見る観点が無い。rule は「処理を続けてよいが、黙らない」と定めているので、
      「理由コメントのある best-effort」でも warn が出ていなければ発見に数えるか、偽陽性の除外の書き方を決める (rule の本文は写さず参照する)
- [ ] **test-cleanup が「消す前に、そのテストだけが捕まえる壊し方を実装に当てて確かめる」を指示していない**。`~/.claude/rules/mutation-verify-new-tests.md` は「既存の assert を『何も検査していない』として消すときも、消す前に変異を 1 本当てる」
      「テスト・describe を『重複』として消すときも同じ」と定めている (後者は 2026-11-17 の commit「docs(rules): テストを消す前の変異・…」で足した)。
      今の test-cleanup の forge のプロンプトと直接実行の指針は `refuse-low-value-coverage.md` の issue 化だけを指す。`bin/mutate-verify` (実装をわざと壊して、残るテストが落ちるかを見る道具) で確かめる段を足す
- [ ] **タイプの一覧が SKILL.md の中の 4 か所 (表・Step 2 のカテゴリ・forge のプロンプト・直接実行の指針) にあり、揃っていることを守る検査が無い**。
      2026-11-18 時点では 22 タイプとも 4 か所に揃っている (表の ID ごとに、プロンプトと指針の行を grep で数えた。カテゴリの行は目で数えた)。
      `tests/` と `scripts/` に `skills/audit` を参照するものは無い (grep で 0 件)。タイプを足す・消すたびに 4 か所を手で直している
      (concurrency の追加 `d3ba8b866` と issues-done の削除が同じ commit)。案: 表を正本にし、残り 3 か所に全 ID が出ることを `make test-lint` の検査で見る。
      all-audit-on-diy は表を読むだけなので、検査の対象は audit の SKILL.md だけでよい
- 局所 (直さない): Step 3 の forge / 直接実行の推奨は 912 (保留) の担当。ここでは触らない

## 2. 足す観点: 入口の一致 (UI 層のビジネスロジック・debug API と UI の経路の差)

### 指示の妥当性の調べ

| 指示 | 判断 | 根拠 |
|---|---|---|
| debug API と UI の差が限りなく小さい | **妥当**。ただし方法は skill `improve-debug-api` に既にある | `improve-debug-api` (2026-11-08、retro 1045 で global に寄せた) が、口ごとに段 (R 読むだけ / A 入力の合成 / B 人の入口と同じ関数 / C debug 専用 / G 安全装置) を決め、人の入口の前段・後段と突き合わせる手順を持つ。audit に同じ手順を写すと 2 本になって片方だけ更新される (`claude-md-maintenance.md` の「同じ規約を複数の場所へ配るときは単一の正本を読み込ませる」)。audit は**差があるかを見つけて improve-debug-api へ渡す**役に留める |
| debug API が通ればアプリの動作確認が完了する | **そのままの形では満たせない。測れる形に言い換える** | `improve-debug-api` 自身が、View / AppKit の側にあって口の経路では構造的に通らない前段を認め、値の節の「残す場所」に載せる形にしている (手順 3 の「分ける」)。描画・レイアウト・フォーカスと responder chain・IME・hover やドラッグのような合成できない入力・アクセシビリティは、B 段の口が同じ関数を呼んでも確かめられない。「完了」を無条件に言うと監査で反証できない (何が漏れても「debug API は通った」で閉じる)。監査で見る形は「**debug API で確かめられない範囲が列挙されていて、それぞれに別の確かめ方 (XCUITest・目視の human issue など) が割り当てられ、その範囲が小さい**」。UI の確認を XCUITest で行う規範は `~/.claude/rules/no-osascript-for-ui-verification.md` (Swift のファイルを読んだときだけ読み込まれる) |
| UI 層にビジネスロジックが無い | **妥当**。上の 2 つの前提になる | View に判定 (押せなくする判定・課金のゲート・範囲の検査・状態の遷移) があると、debug API の口はそれを飛ばす (差になる) か、口の側に写す (重複になる) かのどちらかになる。`improve-debug-api` の手順 5 の 2 が「前段・後段を共通の関数へ移す」と言っているのは、この不変条件を後から直す形。ただし今の audit の encapsulation は「表示・整形を View 側が持つこと」を偽陽性に数えているので、**表示のための判断と、操作の可否・状態を変える判断の境界**を書かないと、同じ箇所が encapsulation では除外・新しい観点では発見になる |
| (適用範囲) debug API がある repo だけ | **広げる**。debug API は「人の入口が複数ある」の一例 | 1070 (2026-11-12) の broken-code の発見は、slack-bbs の購読の画面が断るアーカイブ済みのチャンネルを、CLI の `sub add` 等が入れられた形で、1070 自身が「同じ約束を別の入口が破る形 (画面と CLI・作る側と読む側)」とまとめている。debug API の無い repo (dotfiles の TUI と CLI) でも同じ形の不具合が出ている。debug API に絞ると、この形を拾うタイプが無いままになる |

### 足す形の案

新しいタイプ `entry-parity` (カテゴリは設計) を 1 つ足し、発見を次の型に分類させる (分類できないものは報告しない。既存の L / E / F / K / C と同じ書き方):

- **P1 UI 層の判断**: View・画面の描画やキー処理の中に、操作の可否・入力の検査・状態の遷移・外部への書き込みを決める判断がある。
  除外: 表示のための判断 (どの色・どの行を出すか・書式化)。境界は「その判断を外すと、人の操作で起きる結果 (状態・外部への書き込み) が変わるか」で決める
- **P2 入口ごとに約束が食い違う**: 同じ操作の入口 (画面・CLI・メニュー・ショートカット・debug API) の片方だけが前段 (断り・確認・範囲) や後段 (記録・通知) を持つ。
  入口を全部挙げ、各入口から共通の関数までの前段・後段を並べて比べる
- **P3 debug API の経路の差**: P2 のうち片方が debug API のもの。**中身の突き合わせは skill `improve-debug-api` に任せ**、audit は「その repo の debug API の docs に『improve-debug-api の値』節があるか」
  「前回の improve-debug-api の点検の issue があり、その後に口が増えていないか」を見て、差がありそうなら improve-debug-api を回すよう issue に書く
- **P4 debug API で確かめられない範囲が書かれていない**: debug API の docs の「残す場所」(View の側の前段) の一覧が無い・実物より狭い、または載っている前段に別の確かめ方が割り当てられていない

各件に必須: 該当 (`file:symbol`) / 入口の一覧と走査した母集合 (入口を数えた方法) / 発火条件 (どの入口から何をすると、人には起きない状態が作れる・人には起きることが確かめられないか) / silent に壊れるか compile error で止まるか。

他のタイプとの寄せ先 (書く前に決める):

- 知識の持ち主が具体的な型 (VM・model) で、View がそこから値を取り出して判定している → P1 で拾い、encapsulation の E1 には数えない。encapsulation の偽陽性の「表示・整形を View 側が持つこと」に「操作の可否・状態を変える判断は entry-parity の P1」と 1 行足す
- 同じ判定が画面と CLI に写されている → P2 で拾い、duplication には数えない
- debug API の docs と口の数のずれ (docs が古いだけ) → claude-md-refresh の担当で、ここでは数えない

### 足す前に確かめること

- [ ] SKILL.md の「新しい監査タイプを足す前に、`issues/done/` の bug issue を数えて軸の分布を見る」に従い、done の bug から P1〜P2 の形のものを数える
      (推測で軸を立てない。1070 の 1 件だけでは足りない)。my-products 側 (vlc-multi-video-player・obaket) の improve-debug-api の点検の issue も数える。
      **このマシンには my-products の checkout が見当たらなかった** (`find ~ -maxdepth 7 -name debug-api.md` で 0 件) ので、数える場所を先に決める
- [ ] 数えて、既存タイプの指針への追記 (broken-code に P2・encapsulation に P1 の境界) で足りるなら、新しいタイプは作らずそちらにする。
      新しいタイプにするのは、偽陽性の除外の集合が既存タイプと違うと言えるとき (leaky-abstraction と encapsulation を分けた理由と同じ判断)
- [ ] 足したら、`improve-debug-api` の SKILL.md の冒頭か手順 0 に「audit の entry-parity の P3 から渡される」と 1 行足す (入口の文書。`new-tool-requires-entrypoint-docs.md`)

## 受け入れ条件

- [ ] 1 節の各項目を直すか、直さない理由を書く
- [ ] 2 節の「足す前に確かめること」の数えた結果をこの issue に書き、タイプを足すか既存タイプへ追記するかを決める
- [ ] SKILL.md を変えたら、`~/.claude/CLAUDE.md` のレビュー方針どおり観点を変えた整合チェックを 3 回回し、`bin/skill-eval audit` の結果を commit message に書く
- [ ] all-audit-on-diy は audit の表を正本として読むので、タイプを足したときに all-audit-on-diy の側を直す所が無いかを確かめる

## 関連ファイル

- `_claude/skills/audit/SKILL.md`
- `_claude/skills/improve-debug-api/SKILL.md`
- `_claude/skills/all-audit-on-diy/SKILL.md`
- issue 1070 (slack-bbs の全観点の監査。P2 の実例)・1045 (improve-debug-api を global に寄せた retro)・912 (Step 3 の推奨。保留)

## 進捗

- 2026-11-18: 起票
