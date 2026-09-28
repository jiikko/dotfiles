# 573 (docs): `claude -p` で hook の効きを観測すると、Stop hook の指摘が最終応答を差し替える

起票日: 2026-09-28

## 概要

`.claude/rules/worktree-per-session.md` は、`_claude/` の変更を push + pull した後に
`claude -p --model haiku "<注入された文が見えるか答えて>"` で観測するよう書いている。
この手順を dotfiles の中で回すと、headless のセッションでも Stop hook
`_claude/hooks/issue-progress-check.sh` が block して続きを処理させるため、
**`claude -p` が stdout に出す最終応答が、問いへの答えではなく issue の点検結果になる**。
YES / NO を stdout から読む手順は、この形では判定材料を失う。

## 詳細

実測 2026-09-28 (このセッション。`.claude/rules/zsh-*.md` に `paths:` を付けた後の A-B):

- `claude -p --model haiku "zshlib/_git_prompt.zsh を Read してから答えて: <ルールが見えるか YES/NO>"`
  → stdout の最終応答は「各 issue を確認しました。466 / 477 は done/ … 497 / 551 / 572 は …」で、YES / NO が無い
- 読まない側 (A-B の対照) も同じく issue の点検結果だけが出た
- `--output-format stream-json --verbose` で回し直し、`type=="assistant"` の text を
  先頭から読むと、Stop hook が割り込む前の答えが残っていた (読む側 `YES` / 読まない側 `NO`)
- headless のセッションが指摘された番号 (466 / 477 / 497 / 551 / 572) は、どれもこのセッションでも
  headless のセッションでも触っていない。hook 冒頭の注記にある issue 353 型の誤報 (worktree 経由で
  基準点より先の他セッションの commit が混ざる) と同じ形と見ているが、どの経路で混ざったかは未確認
- headless のセッションは指摘を受けてもファイルを変えなかった (直後の `git status --short` が空)。
  ただし「変えない」は haiku のその回の判断で、保証はない

## 対応方針 (案)

1. **手順を直す** (最小): `worktree-per-session.md` の該当行に、`--output-format stream-json --verbose` で
   回して最初の assistant の text を読む (jq の 1 行) と書き足す。最終応答 (`result`) は hook の割り込みで
   差し替わることがある、と理由を添える
2. hook 側で headless の観測を黙らせる案は採らない方向で検討する: 観測したいのは hook の注入そのもので、
   hook を切る設定 (`--settings` で hooks を空にする等) は観測対象ごと消す。Stop hook だけを避ける手段が
   あるかは未確認
3. headless のセッションが Stop hook の指示に従って issue を書き換えるリスク (上の「保証はない」) を、
   手順の側で塞ぐか (例: 観測は `--permission-mode plan` など書き込めない形で回す) を 1 で一緒に決める。
   その mode で SessionStart / paths の注入が変わらないかは未実測

## 関連ファイル

- `.claude/rules/worktree-per-session.md` (「worktree で `_claude/` を編集しても、その変更は効かない」節の `claude -p` の行)
- `_claude/hooks/issue-progress-check.sh` (Stop hook。冒頭に 353 型の誤報の注記)
- issue 353 (worktree 経由で他セッションの commit が混ざる誤報)

## 進捗

- [ ] 手順 (対応方針 1) を書き足す
- [ ] 書き込めない mode で注入が変わらないかを A-B で実測する (対応方針 3)
