# 962 (docs): dupscan の出力にある機能 (json の欄・`-v` の節) が skill に案内されず、使う側が作り直す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-29〜）

起票日: 2026-10-29

## 概要

dupscan が出力に持っている機能を skill が案内していないので、使う側はそれを知らずに同じことを手で作る。
subcommand と flag は `capabilities` と `agent-guide` のテストが登録と照らしているが、**出力の欄 (json のキー) と `-v` の節**は
どこにも一覧が無く、skill の記述と照らす検査も無い。

## 詳細

### 実際に起きたこと (961 の W1 / W7)

obaket 1308 の作業者は、次の 2 つを「あると便利な機能」として 961 に挙げた。W1 は既にあり、W7 は大半が既にあった。

- W1「json の組に除外の 1 行を入れてほしい」: report の各組と gate の各違反の `exclude` に、hash 付きの釘の除外の 1 行が
  2026-10-25 (`feat(dupscan): 除外の設定 (.dupscan.json) を report と gate に効かせる`) から入っている。作業者は `-v` の text を
  正規表現で切り出し、最初の切り方で別の組のメンバーを拾って組の対応を誤った
- W7「寄せる前後の組の差 (消えた・縮んだ・新しく生じた) を dupscan 自身が出してほしい」: gate の違反が新しい写しを含む組、
  `-v` に消えた組・縮んだ組 (`feat(dupscan): gate で寄せて消えた組を出し、…` 2026-10-25) と新しく足された除外が出る。
  作業者は report の json を 2 回取り、自作の python で hash を突き合わせた (約 6 回)。
  ただし gate は hash の差と同じではない: 前からある写しに同じ修正を入れて別の形へ移った組は違反にしない
  (`internal/gate/gate_test.go` の `TestSameEditToExistingCopiesIsNotViolation`)。組の除外は違反を数える前にかかり、
  「新しく足された除外」は設定の要素 (釘・glob・プリセット) の差で、黙らせた組の一覧ではない。hash の前後の差を全部見たい用途には、
  gate だけでは足りない

skill (`_claude/skills/dupscan/SKILL.md`) の 3 節は、当時「違反の `-v` に出る除外の 1 行」しか書いていなかった。README
(`src/dupscan/README.md` の除外の設定の節・gate の判定の節) には書いてあった。作業者が README を読んだかは 961 に記録が無い (**未確認**)。

### 検査の現状 (2026-10-29 に確かめた範囲)

- `src/dupscan/capabilities_cmd_test.go` の `TestCapabilitiesRegistryAndLanguageSupport` は登録・形式・言語・実際の help との一致を、
  `TestCapabilitiesIncludesActualCommandFlags` は代表の flag の存在を照らす
- `src/dupscan/agent_guide_cmd_test.go` の `TestAgentGuideOnlyAdvertisesExecutableRegisteredCommands` は、`agent-guide` が案内する argv を
  fixture の上で実行し、登録済みで実際に動くことまで照らす
- `agent-guide` の出力は 4 つの argv の例 (search / evidence / 3 番目は Swift と all が context・Go が callers / gate) と、scope・型解決の限界・hook などの注意で、
  出力の欄・`-v` の節は案内しない (`agent_guide_cmd.go` に `exclude` / `resolved` / `消えた組` の語は 0 件)
- skill の記述と dupscan の出力の欄を照らすテストは、`src/dupscan/*_test.go` と `tests/claude/` を `SKILL` / `capabilities` で探した範囲と、
  codex の反証レビュー (2026-10-29。Go のテスト・`tests/`・skill eval を探した) では見つからなかった。skill eval の grader
  (`_claude/skill-evals/dupscan/invoked*/graders/invoked.md`) は skill が起動されたかだけを採点する

## 対応方針 (案。着手する人が反証レビューに通す)

- 出力の欄と `-v` の節の一覧を 1 か所に持つ (候補: `capabilities -format json` に report / gate の出力の欄を足す)。2 つは作り方が違う:
  - json の欄は出力の型から作り、手で写さない。起点は `report.Summary` / `report.GateSummary` で、配列の要素・埋め込みの `Exclusions`・
    `ViolationJSON` を辿る。`commandSpec` (`command_registry.go`) は出力の型を持たないので、subcommand と出力の型の対応を足す。
    `capabilities` の「repo を読まない」「出力の上限」の制約は保つ
  - `-v` の節は json タグに無い (消えた組 / 縮んだ組の分け方・json に出ない `staleWhy` / `staleCandidates`・`exclude` が json を入れた文字列であること)。
    描画の処理と共有する説明のデータを持つ形を、着手時に設計する
- skill の 3 節に「出力の欄の一覧はここで引く」の 1 行を置き、個々の欄の説明は一覧の側に寄せる (skill と README に同じ説明を 2 か所持たない)
- 案内の漏れを機械で止めるなら、「作業者が自作で作り直したもの」を拾う手段が要る。検査で拾えるのは「一覧にある欄が skill から辿れるか」までで、
  作業者が一覧を引かない限り W1 / W7 の形は防げない。どこまでを検査にするかは着手時に決める

## 設計 (2026-10-29。反証レビュー (opus) を通した後の形)

最初の案 (出力の型の各欄に `desc` タグ・`capabilities -format json` に `outputs` を足して schema_version を上げる) は、反証レビューで次の理由で採らなかった:
- schema_version を上げると、`_claude/hooks/suggest-dupscan-for-symbol-grep.sh` の `go_capability` が `.schema_version == 1` を照らしているので、Go の grep の案内が全部「非対応」に落ちる (`-no-build` の古い版もあるので `== 2` に直しても同じ)
- 欄をパスに平たくして能力表に足すと、`capabilities` の 64KB の上限に届く見込み (超えると何も出さず rc=2 で、hook と dogfood の確認が壊れる)
- `desc` タグは raw string でバッククォートを書けず、gopls / godoc に出ず、設定ファイルの入力でもある `exclude.Rule` に出力の説明が混ざる
- 同じ型の欄が出る場所で意味が違う (`UnitRef.allowed` は report と near にだけ入り、gate では出ない) ので、欄に 1 つの説明では事実と違う

採った形:
- 入口は `dupscan capabilities -outputs report|gate` (能力表と schema_version は変えない)。型ごとに 1 回だけ欄を並べ、どの型がどこに出るかを添える。text と json
- 説明は `internal/report` の表 (鍵は `型名.json の欄名`) に置き、Go のコメントは残す。出る場所で意味が違う欄は、説明にそう書く
- `-v` の行は {鍵・見出し・説明・対応する json の欄・subcommand} の表に置き、描く処理は表の鍵を受ける関数でだけ書く
- 検査: (1) reflect で辿った欄の集合と説明の表の鍵が両方向で一致する (2) fixture で出した report / gate の json の鍵が、辿った型の欄に全部ある (3) report package の `-v` の分岐と `-v` 専用の関数の中で、fmt で直に書いていない (AST) (4) `-v` の表の鍵が全部使われ、`-outputs` の名前が登録済みの subcommand
- 正本は一覧の側。skill の 3 節は「欄と `-v` の行の意味は `dupscan capabilities -outputs gate` で引く」の 1 行にし、欄をまたぐ意味 (gate は前後の hash の差と同じではない) だけを残す。README も一覧を指す
- 検出しない形: 欄の意味が変わったのに説明が古いまま (欄の増減しか見ない)・`-v` の分岐から呼ぶ別の関数の中で直に書く行・fixture に出ない json の欄の誤り (型は辿るので、欄の追加は (1) で捕まる)

## 受け入れ条件 (案)

- [ ] report と gate の json の欄 (出力の型から作る) と `-v` の節 (描画と共有する説明から作る) の一覧を、1 か所で引ける
- [ ] skill から、その一覧へ 1 手で辿れる
- [ ] 一覧と出力の型がずれたら落ちる検査がある (変異で赤を確かめる)

## 関連

- [961](done/961-research-dupscan-wishes-from-obaket-1308-2026-10-29.md) の W1 / W7 (起点。961 では skill の 3 節に案内を足すだけにした。961 は 2026-10-29 に done。この issue は継続)
- [932](932-epic-dupscan-accuracy.md) (epic)
- `src/dupscan/capabilities_cmd.go` / `src/dupscan/agent_guide_cmd.go` / `_claude/skills/dupscan/SKILL.md`
