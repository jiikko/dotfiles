# 911 (test): forge / audit のタスク完了 eval を作る (今は「発火するか」の eval しかない)

起票日: 2026-10-25

> **保留** (2026-10-29。ユーザー決定: 「保留して 914〜916 へ」)。0 の前提確認で、forge の出番が 09-16 以降無いことが分かった (下の「0 の結果」)。
> **再開の条件**: audit の実行方式で forge を選ぶ回が戻ってきたとき (目安: audit-log の forge の行が月に数回出る)、または forge を単独で使う場面が増えたとき。
> 再開したら、0 の 3 つ目 (forge Standard の 1 run の実費) から始める

epic [910](../910-design-fold-subagents-into-main-loop.md) の前提。912 / 913 は、この eval で変更の前後を比べてから決める。

## 概要

`_claude/skill-evals/` にあるケースは 13 skill・16 本で (2026-10-29。起票の 10-25 は 12 skill・13 本。その後 dupscan の Go と dupscan-dogfood が足された)、zundamon-kaisetsu の `no-pointing-at-show` の 1 本を除き「skill が発火するか / 選択肢を出すか」しか見ていない
(2026-10-25 時点: `audit/invoked` は `offers-ci` / `offers-concurrency` の grader、`codex-drive/fires` は `fires` の grader。forge はケースが無い)。
fan-out を畳む・残すの判断には「仕込んだ欠陥を見つけられたか」のような**タスク完了**の eval が要る。
記事の実例では、統合で壊れた挙動が **2 回に 1 回しか出なかった**ので、各ケースを複数回回す。

### `bin/skill-eval` はこの用途にそのまま使えない (2026-10-25 に冒頭とコードで確認)

- plugin に写すのは `skills/<skill>/` と `evals/` だけで、各 run は隔離された HOME で動く (ユーザーの CLAUDE.md・rules・他の skill は読まれない)。
  forge が前提にする `~/.claude/workflows/forge.js` と `_claude/agents/*.md` は届かないので、eval の中の forge は本番と別物になる
- 比べるのは「plugin あり / なし (baseline)」の 2 つだけ。912 の ①②③ や 913 の A/B/C のような、skill の版・引数の違いは比べられない。baseline の run は費用の半分を使うが、この比較には意味が無い
- 費用の実測は「1 ケース 1 回ずつで $0.14」で、これは発火を見るだけのケースの値。forge の fan-out を回す run の費用はまだ測っていない

## タスク

### 0. 前提の確認 (fixture を作る前に。どれも 1 本で確かめられる)

- [ ] forge.js から `agentType: <名前>` で起動した専門家が `_claude/agents/<名前>.md` の本文を読み込むかを 1 本で確かめる (913 の「未確認」)。
      読み込まないなら、913 で比べる「専門家の分割」は実在しないことになるので、913 を先に書き直す
- [ ] forge が実際にどれだけ使われているかを確かめる: workflow の agent が transcript にどう残るかを 1 本で確かめ、直近 90 日の forge の起動回数を数える。
      ほとんど使われていないなら 912 / 913 に eval をかける価値が下がるので、着手するかをユーザーに諮る
- [ ] forge Standard の 1 腕・1 run を手で回して、実費・所要・5h 枠を何 % 使うかを測る (fixture はまだ無いので、既存の小さなディレクトリをレビューモードで対象にする。
      5h 枠は起動の前後に `ratelimit` で読む)。この数字で全体の run 数と費用の上限を決める (5h 枠を跨ぐなら日を分ける)

### 1. driver を作る

- [ ] fixture の写し (使い捨ての worktree) の上で、**本物の HOME の `claude -p`** を回す driver を作る (本番の rules・agents・forge.js が読まれる条件で測るため)。
      腕 (skill の版・forge.js の引数・audit の実行方式) を引数で切り替え、baseline は回さない
- [ ] 非対話で止まらないようにする: forge が明示で飛ばせるのは Phase -1 (モード選択) だけで、Phase 1.5 の設計承認や audit の Step 3 は AskUserQuestion を出す。
      各ケースは「1.5 の設計書を書いた時点」「issue を書いた時点」で打ち切る形にし、codex に届く Phase (forge の Phase 5.3 の codex-review) を回さないことを driver で保証する
      (起票時は「codex は明示の指示があるときだけ使う、が記憶にあるユーザーの決定」を根拠にしていたが、2026-10-29 の memory にその項は無い。回さない理由は codex の枠と費用)
- [ ] **audit の forge 腕の打ち切り点を別に決める**: audit から起動した forge は実装モードとして動き (912)、issue を書く前に Phase 1.5 の設計承認 (AskUserQuestion) に当たる。
      driver が承認を前もって与えられるかを確かめ、与えられなければ forge 腕は Phase 1 の出力 (統合された findings) で採点する、と決めて 912 にも書く
- [ ] **本物の HOME では hook も走ることを扱う**: SessionStart の注入 (issue 規約など)・UserPromptSubmit・Stop hook (`issue-progress-check.sh` が block して続きを処理させ、最終応答が差し替わる。
      `.claude/rules/worktree-per-session.md` の headless の注記) が腕の挙動と採点を変える。audit は issues/ を書くので Stop hook に当たる。
      どの hook が走ったかを run ごとに記録し、採点は最終応答ではなく run が書いたファイル・構造化した出力から読む
- [ ] run は 1 本ずつ直列に回す (memory `audit-scans-serial`: 監査の調査体は 1 体ずつ直列。prompt cache を優先するユーザーの決定)。forge の腕は 1 run の中で並行の fan-out を起こすので、その腕を回すことはユーザーに了承を取る

### 2. fixture と正解表

- [ ] **fixture の言語とロスターの対応を決める**: forge のロスターは言語で変わる (`forge.js` の `baseRoster`。Standard は swift が必須 6 (+investigate で research-assistant)、
      go は Explore を含めて 5 体)。`language: 'generic'` には専用の分岐が無く swift の既定に落ちるので、shell の fixture を generic で回すと Swift の専門家が shell を読む。
      Go の fixture にするか、generic の扱いを先に決める
- [ ] **小さい fixture**: 既知の欠陥を仕込み、監査タイプ (concurrency の K1 check-then-act・false-green の F2 対象 0 件が合格・encapsulation の E2 など) と
      forge のレビューで見つかるべきものに対応させる。正解表 (欠陥 ID・file:行) はケースの外に置く
- [ ] **大きい fixture を 1 本**: 実在する大きめの repo の写しに欠陥を数件だけ仕込んだケース。記事がサブエージェントを残す条件 (コンテキストの肥大化) は、小さい fixture では
      起きないので、小さいものだけで測ると直接実行が構造的に有利になる。作れないなら、912 / 913 の結論の適用範囲を fixture の規模に限ると明記する

### 3. 採点と判定規則 (eval を回す前に決めて、この issue に書く)

- [ ] 指摘は欠陥 ID と file:行 を構造化して出させ、正解表との照合はスクリプトで行う (LLM の grader に照合させない)
- [ ] 偽陽性 (正解表に無い指摘のうち、発火条件の無いもの) の判定は LLM に頼るので、同じ出力を 2 回採点させて一致率を記録し、一致しないものは人の抜き取りで決める
- [ ] **ノイズ床を測る**: 腕を比べる前に、同じ腕を 2 回ずつ回したときの検出数の差を測る
- [ ] **判定規則を先に書く**: 何件の差で勝ちとするか / runs と欠陥数をいくつにするか (欠陥 5〜10 件 × runs 3 では、20 ポイントの差でも誤差の 1.4 倍ほどしか出ない見積もり) /
      **判別できないときは「未決」とし、畳まない (現状維持)**
- [ ] 検出数・偽陽性・費用・所要・ターン数を同じ表に残す (記事の比較軸は品質・コスト・遅延の 3 つ)

## 受け入れ条件

- 0 の前提確認の結果がこの issue に書かれ、続けるか止めるかが決まっている
- driver で、forge (レビューモード) と audit (2 タイプ以上) について、検出数・偽陽性数・費用・所要を腕ごとに runs 回ぶん出せる
- 正解表を変えた fixture で、採点のスクリプトが実際に数を変えること (欠陥を 1 件消すと検出数が減ること) を 1 回確かめる
- ノイズ床と判定規則が、腕の比較より前にこの issue に書かれている

## 関連ファイル

- `bin/skill-eval` / `_claude/skill-evals/` (発火の eval。この issue の driver は別に作る)
- `_claude/skills/forge/SKILL.md` / `_claude/workflows/forge.js`
- `_claude/skills/audit/SKILL.md`

## 0 の結果 (2026-10-29)

- **専門家は agent の定義を読み込むか**: frontmatter は効いている (実測)。2026-10-03 の forge の run (swift-smbee、`wf_dc836c15-031`) の各 agent の transcript で、
  `dependency-analyzer` は定義の `model: sonnet` どおり sonnet で 23 ターン、他の専門家は定義どおり opus で走った。本文 (system prompt) は transcript に残らないので直接は見ていないが、
  同じ定義ファイルが `agentType` で読まれている。913 の「未確認」はこの範囲まで閉じた
- **forge の使われ方**: 2 つの源で数えた。
  - audit-log (git で全マシンの分が集まる): 08-22 以降の forge 経路の監査は 13 回 (dotfiles 10・ThumbnailThumb 1・obaket 2)。09-03〜09-16 で、モードは Minimum / Minimum+ が 12 回・Standard が 1 回。
    **09-16 より後は 0 回**で、同じ期間の直接実行は dotfiles で 15 日・obaket で 10 日ある。それより前は forge が多い (promiseApp 133 行・dotfiles-gui 170 行など、6 月以前)
  - transcript (このマシンの分だけ。残っているのは 08-22 以降): forge の workflow の run は 1 本 (10-03、swift-smbee、21 体・約 376 万トークン・18.8 分)。`/audit` の起動は 3 回で、
    実行方式は 3 回とも「直接実行」 (10-13〜10-20 の 2 セッション)
  - ユーザーの補足 (2026-10-29): forge は audit で使うことがあるが、最近は直接実行ばかりなので出番が無いのかも
- **1 run の実費**: 測っていない (保留にしたため)。上の 10-03 の run が規模の目安 (21 体・約 376 万トークン)

## 進捗

- 2026-10-29: 本文を今のコードと突き合わせた。ケースの数 (13 skill・16 本)、codex を回さない根拠 (memory に無い) を直した。`bin/skill-eval` の仕組み・forge.js のロスター (`baseRoster`)・
  forge の Phase -1 / 1.5 / 5.3・audit の Step 3 は本文どおりだった。0 の前提確認を 2 つ行い (上)、結果をユーザーに諮って保留にした
