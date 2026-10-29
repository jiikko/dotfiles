# 962 (docs): dupscan の出力にある機能 (json の欄・`-v` の節) が skill に案内されず、使う側が作り直す

起票日: 2026-10-29

## 概要

dupscan が出力に持っている機能を skill が案内していないので、使う側はそれを知らずに同じことを手で作る。
subcommand と flag は `capabilities` と `agent-guide` のテストが登録と照らしているが、**出力の欄 (json のキー) と `-v` の節**は
どこにも一覧が無く、skill の記述と照らす検査も無い。

## 詳細

### 実際に起きたこと (961 の W1 / W7)

obaket 1308 の作業者は、次の 2 つを「あると便利な機能」として 961 に挙げた。どちらも既にあった。

- W1「json の組に除外の 1 行を入れてほしい」: report の各組と gate の各違反の `exclude` に、hash 付きの釘の除外の 1 行が
  2026-10-25 (`feat(dupscan): 除外の設定 (.dupscan.json) を report と gate に効かせる`) から入っている。作業者は `-v` の text を
  正規表現で切り出し、最初の切り方で別の組のメンバーを拾って組の対応を誤った
- W7「寄せる前後の組の差 (消えた・縮んだ・新しく生じた) を dupscan 自身が出してほしい」: gate の違反が新しく生じた組、`-v` に消えた組・縮んだ組と
  新しく足された除外が出る。作業者は report の json を 2 回取り、自作の python で hash を突き合わせた (約 6 回)

skill (`_claude/skills/dupscan/SKILL.md`) の 3 節は、当時「違反の `-v` に出る除外の 1 行」しか書いていなかった。README
(`src/dupscan/README.md` の除外の設定の節・gate の判定の節) には書いてあった。作業者が README を読んだかは 961 に記録が無い (**未確認**)。

### 検査の現状 (2026-10-29 に確かめた範囲)

- `src/dupscan/capabilities_cmd_test.go` の `TestCapabilitiesIncludesActualCommandFlags` などは、登録した subcommand と flag の一覧を照らす
- `src/dupscan/agent_guide_cmd_test.go` の `TestAgentGuideOnlyAdvertisesExecutableRegisteredCommands` は、`agent-guide` が案内する argv が登録済みかを照らす
- `agent-guide` の出力は search / evidence / context / gate の 4 つの使い方だけで、出力の欄・`-v` の節は案内しない (`agent_guide_cmd.go` に `exclude` / `resolved` / `消えた組` の語は 0 件)
- skill の記述と dupscan の出力の欄を照らすテストは、`src/dupscan/*_test.go` と `tests/claude/` を `SKILL` / `capabilities` で探した範囲では見つからなかった
  (**未確認**: 別の名前で照らしている検査がある可能性は消していない)

## 対応方針 (案。着手する人が反証レビューに通す)

- 出力の欄と `-v` の節の一覧を 1 か所に持つ (候補: `capabilities -format json` に report / gate の出力の欄を足す)。一覧は出力の型 (`report.GroupJSON` / `report.GateSummary` の json タグ) から作り、手で写さない
- skill の 3 節に「出力の欄の一覧はここで引く」の 1 行を置き、個々の欄の説明は一覧の側に寄せる (skill と README に同じ説明を 2 か所持たない)
- 案内の漏れを機械で止めるなら、「作業者が自作で作り直したもの」を拾う手段が要る。検査で拾えるのは「一覧にある欄が skill から辿れるか」までで、
  作業者が一覧を引かない限り W1 / W7 の形は防げない。どこまでを検査にするかは着手時に決める

## 受け入れ条件 (案)

- [ ] report と gate の json の欄と `-v` の節の一覧を、出力の型から作って 1 か所で引ける
- [ ] skill から、その一覧へ 1 手で辿れる
- [ ] 一覧と出力の型がずれたら落ちる検査がある (変異で赤を確かめる)

## 関連

- [961](961-research-dupscan-wishes-from-obaket-1308-2026-10-29.md) の W1 / W7 (起点。961 では skill の 3 節に案内を足すだけにした)
- [932](932-epic-dupscan-accuracy.md) (epic)
- `src/dupscan/capabilities_cmd.go` / `src/dupscan/agent_guide_cmd.go` / `_claude/skills/dupscan/SKILL.md`
