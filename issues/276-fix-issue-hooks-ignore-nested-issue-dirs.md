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

## 受け入れ条件

- [ ] obaket の `macOS/issues/` に置いた期限切れ human issue が `human-tasks-due` に出る (fixture)
- [ ] 4 hook のテストに入れ子 dir のケースがあり、変異で red
- [ ] `issues/` が無く `*/issues` だけの repo でも動く / どちらも無い repo では今までどおり無音

## 関連

- obaket `issues/719-retro-epic-dirs-tooling-2026-09-05.md` (起源)
