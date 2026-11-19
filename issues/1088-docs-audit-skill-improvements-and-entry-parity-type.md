# 1088 (docs): audit skill の改善点と、UI 層・debug API の経路の一致を見る観点の追加

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-19〜）

起票日: 2026-11-18

## 概要

`_claude/skills/audit/SKILL.md` (v1.5.0・22 タイプ) を読み直し、直す点と足す観点をまとめる。
ユーザー指示 (2026-11-18) で、「debug API (UI を通さずにプログラムからアプリを操作する層) がある repo では、UI との差が限りなく小さく、
debug API が通れば動作確認が済む」「UI 層にビジネスロジックが無い」を見る観点を足すことも頼まれた。指示の妥当性を調べた結果を下の 2 節に書く。

## 1. 直す点 (今の SKILL.md と実態のずれ・抜け)

- [x] **`ci` と `concurrency` の「まだ実走していない」が古い**。Step 2 の下の注記 2 つ。dotfiles の `issues/audit-log` には ci が 16 行、concurrency が 18 行ある
      (2026-11-18 に `awk -F'\t' '{print $2}' issues/audit-log | sort | uniq -c` で数えた)。直近の 1070 (src/slack-bbs の全観点の監査) では
      ci 0 件 (CI の paths の追加あり)・concurrency 0 件 (記録 1) だった。注記を「初回に見ると書いた C1〜C4 / K1〜K4 の分類が使えたか・偽陽性が出たか」の結果に置き換える。
      結果を数えるには audit-log の出力ファイル (issue) を読む必要があり、ここではまだ読んでいない
- [x] **error-handling の指針が `~/.claude/rules/warn-log-on-unexpected-behavior.md` (ユーザー指示 2026-10-30、rule の追加は `c1b6f7da1` 2026-10-31) を見ていない**。今の指針は「握りつぶされたエラー」を探すが、
      best-effort で続けてよい失敗に warn のログが出ているかを見る観点が無い。rule は「処理を続けてよいが、黙らない」と定めている。
      error-handling の指針そのものには偽陽性の除外の節が無く、rule とぶつかるのは false-green の除外「理由コメントのある best-effort」と
      concurrency の除外「理由コメント付きの best-effort な kill・後始末」の方。error-handling に「warn が出ていない握り潰し」を足し、
      その 2 つの除外が warn の有無を問わないことと矛盾しないよう書き分ける (rule の本文は写さず参照する)
- [x] **test-cleanup が「消す前に、そのテストだけが捕まえる壊し方を実装に当てて確かめる」を指示していない**。`~/.claude/rules/mutation-verify-new-tests.md` は「既存の assert を『何も検査していない』として消すときも、消す前に変異を 1 本当てる」
      「テスト・describe を『重複』『委譲だけ』として消す・統合するときも同じ」と定めている (後者は 2026-11-17 の commit「docs(rules): テストを消す前の変異・…」で足した)。
      今の test-cleanup の forge のプロンプトと直接実行の指針は `refuse-low-value-coverage.md` の issue 化だけを指す。`bin/mutate-verify` (実装をわざと壊して、残るテストが落ちるかを見る道具) で確かめる段を足す
- [x] **タイプの一覧と発見の型が複数の場所に写されていて、揃っていることを守る検査が一部にしか無い**。写しの場所:
      - audit の SKILL.md の中の 5 か所: 表・Step 2 のカテゴリ・forge のプロンプト・直接実行の指針・直接実行の「設計系 (design / … / ui-components) をまとめて回すときは」の段落 (型の例 L1〜L7 / E1〜E4 も持つ)
      - all-audit-on-diy の SKILL.md の冒頭: 「発見の型 (L1〜L7 / E1〜E4 / F1〜F5 / K1〜K4 / C1〜C4)」。型を足すとここが古いまま残る
      - 2026-11-18 時点では 22 タイプとも最初の 4 か所に揃っている (表の ID ごとに、プロンプトと指針の行を grep で数えた。カテゴリの行は目で数えた)。
        タイプを足す・消すたびに手で直している (concurrency の追加と issues-done の削除は同じ commit `d3ba8b866`)
      - 守る検査: `tests/` と `scripts/` に `skills/audit` を参照するものは無い (grep で 0 件)。ただし skill-eval の `_claude/skill-evals/audit/invoked/graders/` の
        `offers-ci.md`・`offers-concurrency.md` が、一覧に ci と concurrency が出ることを見ている (`bin/skill-eval audit` で回る。CI では回さない)
      - 案: 表を正本にし、残りの場所に全 ID が出ることを `make test-lint` の検査で見る。all-audit-on-diy の型の写しは、写さずに「audit の発見の型」と参照する形に直す。
        検査を足すなら、上の 2 本の grader と同じことを見ていないかを決める (CI で回るのは lint の方だけ)。
        912 が再開して forge の経路を減らすと「forge のプロンプト」の節が変わるので、検査は 912 の結論の後に足すか、節が無くなっても壊れない形にする
- 局所 (直さない): Step 3 の forge / 直接実行の推奨は 912 (保留) の担当。ここでは触らない

## 2. 足す観点: 入口の一致 (UI 層のビジネスロジック・debug API と UI の経路の差)

### 指示の妥当性の調べ

| 指示 | 判断 | 根拠 |
|---|---|---|
| debug API と UI の差が限りなく小さい | **妥当**。ただし方法は skill `improve-debug-api` に既にある | `improve-debug-api` (2026-11-08、retro 1045 で global に寄せた) が、口ごとに段 (R 読むだけ / A 入力の合成 / B 人の入口と同じ関数 / C debug 専用 / G 安全装置) を決め、人の入口の前段・後段と突き合わせる手順を持つ。audit に同じ手順を写すと 2 本になって片方だけ更新される (`claude-md-maintenance.md` の「同じ規約を複数の場所へ配るときは単一の正本を読み込ませる」)。audit は**差があるかを見つけて improve-debug-api へ渡す**役に留める |
| debug API が通ればアプリの動作確認が完了する | **そのままの形では満たせない。測れる形に言い換える** | `improve-debug-api` 自身が、View / AppKit の側にあって口の経路では構造的に通らない前段を認め、値の節の「残す場所」に載せる形にしている (手順 3 の「分ける」)。描画・レイアウト・フォーカスと responder chain・IME・hover やドラッグのような合成できない入力・アクセシビリティは、B 段の口が同じ関数を呼んでも確かめられない。「完了」を無条件に言うと監査で反証できない (何が漏れても「debug API は通った」で閉じる)。監査で見る形は「**debug API で確かめられない範囲が列挙されていて、それぞれに別の確かめ方 (XCUITest・目視の human issue など) が割り当てられ、その範囲が小さい**」。UI の確認を XCUITest で行う規範は `~/.claude/rules/no-osascript-for-ui-verification.md` (Swift のファイルを読んだときだけ読み込まれる) |
| UI 層にビジネスロジックが無い | **妥当**。上の 2 つの前提になる | View に判定 (押せなくする判定・課金のゲート・範囲の検査・状態の遷移) があると、debug API の口はそれを飛ばす (差になる) か、口の側に写す (重複になる) かのどちらかになる。`improve-debug-api` の手順 5 の 2 が「前段・後段を共通の関数へ移す」と言っているのは、この不変条件を後から直す形。ただし今の audit の encapsulation は「表示・整形を View 側が持つこと」を偽陽性に数えているので、**表示のための判断と、操作の可否・状態を変える判断の境界**を書かないと、同じ箇所が encapsulation では除外・新しい観点では発見になる |
| (適用範囲) debug API がある repo だけ | **指示どおり debug API がある repo に絞る。広げるかは数えてからユーザーに聞く** | debug API は「人の入口が複数ある」の一例ではある。1070 (2026-11-12) では、slack-bbs の購読の画面が断るアーカイブ済みのチャンネルを CLI の `sub add` 等が入れられた (画面と CLI の 2 つの入口で約束が食い違う形)。ただしこの 1 件は既存の **broken-code** で見つかり、その場で直っている。既存タイプで拾えた実例 1 件では、広げる根拠にならない。下の「足す前に確かめること」で数えた結果を持って、ユーザーに範囲を確かめる |

### 足す形の案

新しいタイプ `entry-parity` (カテゴリは設計) を 1 つ足し、debug API がある repo で回す (無い repo では「対象外」と記録して飛ばす。範囲を広げるかは上の表のとおり数えてから聞く)。発見は次の型に分類させる (分類できないものは報告しない。既存の L / E / F / K / C と同じ書き方):

- **P1 UI 層の判断**: View・画面の描画やキー処理の中に、操作の可否・入力の検査・状態の遷移・外部への書き込みを決める判断がある。
  除外: 表示のための判断 (どの色・どの行を出すか・書式化)。境界は「その判断を外すと、人の操作で起きる結果 (状態・外部への書き込み) が変わるか」で決める
- **P2 入口ごとに約束が食い違う**: 同じ操作の入口 (画面・CLI・メニュー・ショートカット・debug API) の片方だけが前段 (断り・確認・範囲) や後段 (記録・通知) を持つ。
  入口を全部挙げ、各入口から共通の関数までの前段・後段を並べて比べる
- **P3 debug API の経路の差・debug API で確かめられない範囲**: P2 のうち片方が debug API のものと、debug API の docs の「残す場所」(View の側の前段) が実物より狭いもの。
  **中身の突き合わせは skill `improve-debug-api` に任せる** (残す場所の確かめも、improve-debug-api の手順 3 の「分ける」が既に持つ)。audit は
  「その repo の debug API の docs に『improve-debug-api の値』節があるか」「前回の improve-debug-api の点検の issue があり、その後に口が増えていないか」だけを見て、
  点検が無い・古いなら improve-debug-api を回すよう issue に書く
- 「残す場所」に載った前段に、debug API 以外の確かめ方 (XCUITest・human issue) が割り当てられているかは、improve-debug-api の手順 3 は「その前段をテストが見ているかも書く」までで、割り当てまでは求めていない。
  ここを足すなら audit ではなく improve-debug-api の手順に足す (単一の正本)

各件に必須: 該当 (`file:symbol`) / 入口の一覧と走査した母集合 (入口を数えた方法) / 発火条件 (どの入口から何をすると、人には起きない状態が作れる・人には起きることが確かめられないか) / silent に壊れるか compile error で止まるか。

他のタイプとの寄せ先 (書く前に決める):

- 知識の持ち主が具体的な型 (VM・model) で、View がそこから値を取り出して判定している → P1 で拾い、encapsulation の E1 には数えない。encapsulation の偽陽性の「表示・整形を View 側が持つこと」に「操作の可否・状態を変える判断は entry-parity の P1」と 1 行足す
  (これは E1 の範囲を狭める変更なので、encapsulation の E1 の説明も同じ commit で直す)
- 同じ判定が画面と CLI に写されている → P2 で拾い、duplication には数えない
- 入口の片方だけが断りを持つ形は、今は broken-code の「呼び出し側と受け側で契約が食い違う」で拾われている (1070)。P2 へ移すなら、broken-code の指針から何を外すかを同じ commit で書く。移さずに broken-code に「入口ごとの約束の食い違い」を足すだけで足りるなら、新タイプは要らない
- debug API の docs と口の数のずれ (docs が古いだけ) → claude-md-refresh の担当で、ここでは数えない

### 足す前に確かめること

- [x] SKILL.md の「新しい監査タイプを足す前に、`issues/done/` の bug issue を数えて軸の分布を見る」に従い、done の bug から P1〜P2 の形のものを数える
      (推測で軸を立てない。1070 の 1 件だけでは足りない)。my-products 側 (vlc-multi-video-player・obaket) の improve-debug-api の点検の issue も数える。
      **このマシンには my-products の checkout が見当たらなかった** (`find ~ -maxdepth 7 -name debug-api.md` で 0 件) ので、数える場所を先に決める
- [ ] 数えて、既存タイプの指針への追記 (broken-code に P2・encapsulation に P1 の境界) で足りるなら、新しいタイプは作らずそちらにする。
      新しいタイプにするのは、偽陽性の除外の集合が既存タイプと違うと言えるとき (leaky-abstraction と encapsulation を分けた理由と同じ判断)
- [ ] 足したら、`improve-debug-api` の SKILL.md の冒頭か手順 0 に「audit の entry-parity の P3 から渡される」と 1 行足す (入口の文書。`new-tool-requires-entrypoint-docs.md`)

### 数えた結果 (2026-11-19)

- 母集合: dotfiles の done の bug issue 240 件 (`ls issues/done/*-bug-*.md issues/epic/*/done/*-bug-*.md | wc -l`)。
  語 (`入口|画面と CLI|CLI と画面|画面では|画面だけ|キーだけ|debug API|View の中|画面の側|片方の入口|別の入口`) の grep で 39 件に絞り、本文を読んで分けた。
  語で拾えない書き方の issue は数えていない (下限)
- **P2 (同じ操作の入口の片方だけが前段・後段を持つ): 4 件**
  - 1070 (research。slack-bbs の CLI `sub add` 等が、購読の画面 (`subs/picker.go`) が断るアーカイブ済みのチャンネルを入れた)。broken-code で見つかった
  - 860 (lan-manager。キー `s` が組む HostKeyAlias が、初期設定の `setup.Dest.Validate` の検査を通らない)。security で見つかった
  - 763 (issue を done へ送る入口が glogx の viewer と `scripts/issue_done.sh` の 2 つで、バナーを外すのは viewer だけ)。監査ではなく作業中に見つけた (本文に監査の出典が無い)
  - 511 (pro-con。画面の issue の一覧から足したカードだけ、依頼の原文と issue の紐づけが無い)。dogfooding で踏んだ
- **P1 (原因が UI 層の判断): 上の 4 件のうち 2 件** (1070: 断りが画面の picker にあり、CLI の直しは判定を写した / 860: 画面の model の `keyList` がその場で値を組む)。
  763 の原因は同じ操作の 2 実装 (Go と shell)、511 は backend の入口ごとの組み立てで、UI 層の判断ではない
- 近いが数えないもの: 664・122 (キーが一部の画面・モードで無言。入口が欠ける形で、前段・後段の差ではない。ui-components / ux で拾われた) /
  245 (同じ入口の中で確認の表示と実行が食い違う) / 244 (案内と挙動) / 964 (同じ状態を 2 か所が別の判定で読む) / 824 (読めないときの扱い) /
  193 (データの出所 (snapshot と live) ごとの検査漏れ。人の入口ではない)。残り 28 件は「入口」を別の意味 (入口の文書・入口で無害化) で使っているだけ
- **debug API の例: 0 件**。dotfiles に debug API を持つ repo は無い (`improve-debug-api の値` の節は dotfiles に無い)。
  my-products (vlc-multi-video-player・obaket) はこのマシンに checkout が無く、数えていない
- 読み: P2 の 4 件は見つけ方が broken-code・security・作業中・dogfooding に散り、どの既存タイプも「入口を全部挙げて前段・後段を並べる」走査を持っていない。
  偽陽性の除外 (表示のための判断・debug 専用の安全装置) は broken-code にも encapsulation にも無い集合

## 受け入れ条件

- [ ] 1 節の各項目を直すか、直さない理由を書く
- [ ] 2 節の「足す前に確かめること」の数えた結果をこの issue に書き、タイプを足すか既存タイプへ追記するかを決める
- [ ] SKILL.md を変えたら、`~/.claude/CLAUDE.md` のレビュー方針どおり観点を変えた整合チェックを 3 回回し、`bin/skill-eval audit` の結果を commit message に書く
- [ ] タイプ・型を足したら、all-audit-on-diy の冒頭の型の写し (L1〜L7 / … / C1〜C4) と skill-eval の graders を同じ commit で直す (1 節の写しの項)

## 関連ファイル

- `_claude/skills/audit/SKILL.md`
- `_claude/skills/improve-debug-api/SKILL.md`
- `_claude/skills/all-audit-on-diy/SKILL.md`
- issue 1070 (slack-bbs の全観点の監査。P2 の実例)・1045 (improve-debug-api を global に寄せた retro)・912 (Step 3 の推奨。保留)

## 進捗

- 2026-11-18: 起票。opus の読み取りのみの反証レビューで 7 件の指摘 (タイプの写しの数え漏れ・best-effort の除外の場所・引用の省略・
  1070 を範囲を広げる根拠にした判断・P4 と improve-debug-api の二重化・broken-code と E1 への影響・912 との依存)。全部を実物で確かめて本文に反映した。
  反証できなかったもの: audit-log の件数・4 か所の一致・「debug API が通れば完了」をそのままでは満たせないという判断・「UI 層にビジネスロジックが無い」が前提になるという整理
- 2026-11-19: 1 節の 1 項目目 (ci / concurrency の注記)。audit-log の出力 issue (697・702・737・747・754・760・844・865・886・984・992・1019・1025・1026・1035・1065・1070) の
  ci / concurrency の行を読んだ。
  - ci (16 回): C1 = 760 (pty テストの Skip が見えない)・1070 (ctxwait の paths を 7 workflow に足した) / C2 = 886 の I11 (lint の共通部だけの commit で 24 プロジェクトの test が走る。受け入れ) /
    C3 = 886 の H1 (setup-go のキャッシュ鍵の共有) / C4 = 844 (test-budget の 10 秒を 842 に添えた)。他の回は 0 件で、多くは C1 の paths と replace の突き合わせだけ (992 は「未実施」)
  - concurrency (18 回): K1 = 749・1070 (記録のみ。lastposts.json の後勝ち) / K3 = 760 (`Reclaim` の子の時間の上限。未確認)・886 の A4・A10・A11 / K4 = 886 の A5・A7・I6 / 697 の data race。K2 は 0
  - どの回も「分類できない」「偽陽性だった」とは書いていない。SKILL.md の注記をこの結果に置き換えた
- 2026-11-19: 1 節の 2 項目目。error-handling の forge のプロンプトと直接実行の指針に「続けてよい失敗でも warn が理由つきで出ていなければ発見」を足し、rule は参照だけにした。
  false-green の除外 (プロンプトと指針の 2 か所) と concurrency の除外 (指針だけ) の「理由コメントのある best-effort」には「warn の有無は error-handling が見る」を添え、除外がその型の範囲の話だと分かるようにした
- 2026-11-19: 1 節の 3 項目目。test-cleanup の forge のプロンプトと直接実行の指針の両方に、消す・統合する前に mutate-verify で残るテストが落ちるかを確かめる段を足した (落ちないなら消さない)
- 2026-11-19: 1 節の 4 項目目。
  - all-audit-on-diy の冒頭の型の写し (L1〜L7 / … / C1〜C4) を「audit skill の直接実行の指針にある」の参照に直した。`_claude/` と `docs/` に他の写しは無い (`grep -rn 'L1〜L7\|K1〜K4\|E1〜E4'`)
  - `tests/claude/test_audit_skill_type_copies.sh` を足した (`make test` の test-discovered が `find tests -name 'test_*.sh'` で拾う)。表の ID を正本に、カテゴリの行・forge のプロンプト・直接実行の指針・設計系の段落の括弧の 4 つを比べる。
    forge のプロンプトは `[モード名]` の行がどこにも無いときだけ飛ばす (912 で forge の節が消えても壊れない。見出しを書き換えただけなら落とす)。
    skill-eval の grader 2 本とは見るものが違う (grader は Claude が一覧を出すかの振る舞い、こちらは文書の写しの一致) ので両方残す
  - 変異 (コピーを `AUDIT_SKILL_MD` で渡す) で red を確かめた: カテゴリの行から concurrency を抜く / forge の ci の行を崩す / 指針の ci の行を崩す / 設計系の括弧から encapsulation を抜く / 表に行を足す / design のカテゴリを品質に変える / forge の見出しだけを変える → どれも rc=1。forge の節ごと消す → note を出して rc=0 (意図どおり)
- 2026-11-19: 写しの検査の敵対レビュー (opus・読み取りのみ。commit 35d1cb88e)。指摘 7 件:
  - 直した 5 件 (どれも直す前の検査で再現した): ① forge のプロンプトの節を消しても Step 3 に forge の選択肢が残っていれば緑 → 選択肢が残っていれば落とす /
    ② 表の区切り行を `|:---|` にすると `:---` を ID に数える / ③ 数字・大文字・`_` を含む ID を抽出しない / ④ 節の中のコードブロックの `# ` 行で節が切れる / ⑤ カテゴリの行の末尾の空白で抽出できない
  - 記録だけ: ⑥ 指針を `###` の子見出しに分けると赤になる (赤なので見逃しにはならない。検査の冒頭の「検出しない形」に書いた) / ⑦ 写しの中身 (説明の文) は比べない (設計どおり)
  - 壊せなかった観点 (レビューの報告): 1 つの写しの直し忘れ・同じ ID を 2 カテゴリに書く・新しいカテゴリの入れ忘れ・見出しだけの書き換え・`set -e` と `|| true` の組み合わせ・CI での実行 (Tests の workflow が拾う)
