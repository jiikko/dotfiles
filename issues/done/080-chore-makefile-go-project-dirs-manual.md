# 080 chore: Makefile の GO_PROJECT_DIRS 手動列挙で新しい src/* が lint / test から黙って落ちる

起票日: 2026-08-21
種別: chore
優先度: **P3** (現状は漏れなし。新しい Go プロジェクトを足した瞬間に無音で外れる)

出典: 監査 [071](071-research-design-audit-2026-08-20.md) の `071-go-project-dirs`。
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

## 対応 (2026-08-25)

`GO_PROJECT_DIRS` を `src/*/go.mod` の発見 (`$(patsubst %/,%,$(dir $(wildcard src/*/go.mod)))`) へ
変え、`test-go-lint` / `test-go` の先頭に**発見 0 件で失敗する**ガードを置いた。発見結果は
従来の手動列挙と一致 (4 プロジェクト)。

### 変異検証 (issue の指示どおり実施)

`src/zz-mutation-probe/` に「lint が必ず exit 1 するダミー Go プロジェクト」を一時的に置いて確認した:

| 形 | 結果 |
|---|---|
| 新形式 (発見) | ダミーを検知して **失敗** ✓ |
| 旧形式 (手動列挙を `make GO_PROJECT_DIRS=...` で模す) | **exit 0 で無音の見逃し** = 欠陥が実在した証拠 |
| 発見 0 件 (`GO_PROJECT_DIRS=""`) | ガードが **失敗させる** ✓ |

ダミーは確認後に削除済み (`src/` は 4 プロジェクトのまま)。

🚨 **CI 側の非対称は残っている**: `.github/workflows/src_<project>.yml` (caller) は今も手動作成で、
新しい `src/foo` を切って yml を作り忘れると **CI では回らない** (ローカルの `make test` では
回るようになった)。この issue の範囲外なので別途。
