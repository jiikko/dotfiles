# 276 fix: issue hooks が `<repo>/issues/` しか走査せず、`macOS/issues/` 等の入れ子 issue dir の human / retro / claim が不可視

起票日: 2026-09-06
種別: fix

## 何が起きるか

`_claude/hooks/lib/issue-hooks.sh` は repo root 直下の `issues/` だけを `ISSUE_HOOK_DIR` にする。obaket は
`macOS/issues/` (macOS 配下だけで完結する issue の置き場。`apps/obaket/.claude/rules/issue-placement.md`) を正式に持っており、
そこに置かれた `NNN-human-*` の期限切れ / `NNN-retro-*` の未決着 / `next/` の claim は `human-tasks-due` / `retro-open` /
`next-claim-push` / `next-claim-unshared` のどれにも見えない (obaket 719 の lens B B3。変更前からの穴、現状は該当 0 件なので実害未発生)。

## 直し方の候補

- `issue-hooks.sh` が `ISSUE_HOOK_DIRS` (複数) を返す。候補は `<root>/issues` に加えて `<root>/*/issues` (1 段) を実在チェック
- 各 hook は dir を for で回す (epic の 2 段 glob はそのまま)
- テスト `tests/claude/test_{human_tasks_due,retro_open,next_claim_push,next_claim_unshared}.sh` に入れ子 dir の fixture を足し、
  走査を 1 段に戻す変異で red

## 対応 (2026-09-06)

`issue_hook_resolve_dir` が **`ISSUE_HOOK_DIRS`（改行区切り）** を返すようにし、
`<root>/issues`・`<root>/issue` に加えて **`<root>/*/issues`・`<root>/*/issue`（1 段）** を拾う。
深さを 1 段に限るのは、全走査にすると `node_modules` / `.git` / ビルド生成物まで舐めるため。
`ISSUE_HOOK_DIR`（単数）は後方互換で最初の 1 つを指す。

4 hook すべてを複数 dir 対応にした（`human-tasks-due` / `retro-open` は dir を回す形へ、
`next-claim-push` / `next-claim-unshared` は `next_dir` の探索へ入れ子を追加）。

### 🚨 実装中に踏んだ 3 つ

1. **`$'\n'` を二重引用符の中に書いた** → ANSI-C 展開されずリテラルになり、区切りが壊れて
   全ファイルが 1 行に潰れた。既存コードは引用符の**外**で連結している（`"..."$'\n'`）
2. **`case "$f" in "$dir"/pending/*` の `$dir` をループ変数に再利用した** → ループ後に空になり
   `[保留]` マーカーが 1 つも付かなくなった。複数 dir では単一の `$dir` に依存できないので、
   パターンを `*/pending/*` の**パス断片**へ変えた
3. **`git status --porcelain -- */issues/next ...` の pathspec** → そのパスが存在しない repo では
   git が pathspec エラーで**空を返す**ので、root 直下の claim まで丸ごと見えなくなった
   （4 ケースが無出力になって気づいた）。pathspec をやめて grep で絞る形へ。
   🚨 そのとき `(^|/)` のアンカーを足したのも誤りで、porcelain は `?? issues/next/` のように
   空白が前に来るため 1 件も拾わなくなった。**元の無アンカー正規表現がもともと入れ子を
   拾える**ので、pathspec を外すだけが本当の修正だった

### 受け入れ条件（実施結果）

- [x] 入れ子 dir に置いた期限切れ human issue が `human-tasks-due` に出る（fixture 追加）
- [x] 4 hook のテストに入れ子 dir のケースがあり、**走査を 1 段に戻す変異で red**
- [x] `issues/` が無く `*/issues` だけの repo でも動く / どちらも無い repo では今までどおり無音

## 受け入れ条件（起票時）

- [ ] obaket の `macOS/issues/` に置いた期限切れ human issue が `human-tasks-due` に出る (fixture)
- [ ] 4 hook のテストに入れ子 dir のケースがあり、変異で red
- [ ] `issues/` が無く `*/issues` だけの repo でも動く / どちらも無い repo では今までどおり無音

## 関連

- obaket `issues/719-retro-epic-dirs-tooling-2026-09-05.md` (起源)
