# 080 chore: Makefile の GO_PROJECT_DIRS 手動列挙で新しい src/* が lint / test から黙って落ちる

起票日: 2026-08-21
種別: chore
優先度: **P3** (現状は漏れなし。新しい Go プロジェクトを足した瞬間に無音で外れる)

出典: 監査 [071](done/071-research-design-audit-2026-08-20.md) の `071-go-project-dirs`。
**出典 issue には「反証で崩れた (却下)」の一覧がある**ので、同型の指摘を再提案する前に読むこと。

## 確認できた事実 (2026-08-21)

- `Makefile:268` — `GO_PROJECT_DIRS := src/parallel-each src/glogx src/disassemble_excel`
  (手動列挙)。:272 / :279 の lint / test ループがこの変数だけを回す
- `src/` の実体も同じ 3 つ。**今は漏れていない**
- この repo は他の全域で「登録なしで対象になる」を徹底している:
  shellcheck 対象は `scripts/discover_shell_scripts.sh` による発見、テストは `test-dir` の
  自動発見。Go だけが手動なので非対称

失敗モードは無音: 新しく `src/foo` を切っても `make lint` / `make test` は緑のまま通り、
「lint も test も通っている」と読める。

## 対応方針

`src/*/go.mod` (または `src/*/Makefile`) の存在で発見する形へ変える。**発見 0 件を fail に
する**こと (発見式のゲートが 0 件で緑になるのは `adversarial-review-own-safeguards.md` が
禁じる false green)。

## 変異検証

`src/` にダミーの Go プロジェクト (わざと `go vet` を落とす内容) を一時的に置き、
`make lint` が赤になることを確認する。現行 Makefile では緑になるはず = それが this issue の
再現。確認後にダミーを消す。

## trigger

新しい Go プロジェクトを `src/` に足すとき、または Makefile の lint / test ターゲットを
次に触るとき。単独でも 30 分程度で閉じられる。
