# glogx: エディタ差し替え点が 3 系統に散り、「何を開いたか」を検証できないテストが量産される

起票日: 2026-08-13

`runEditorCmd`（`tea.ExecProcess` の差し替え点）をテストで置き換える方法が 3 系統ある。

| 系統 | 場所 | 見えるもの | 利用数 |
|---|---|---|---|
| `stubEditor(t) *int` | `tui_helpers_test.go` | 呼ばれた**回数だけ** | 2 |
| `stubEditorCapture(t) *[]*exec.Cmd` | `open_workspace_test.go` | 実行された `*exec.Cmd` | 6 |
| `runEditorCmd = func(...)` の直書き | 各テスト | その場しだい | 5 |

## 何が問題か

`stubEditor` は `*exec.Cmd` を丸ごと捨てるため、**「エディタが呼ばれた」ことしか assert できない**。
つまりそれを使うテストは、開く対象を取り違えても green になる。実測（2026-08-13 の敵対レビュー）:

- `issuesView.editCmd` の `editorCommand(iss.Path)` を `editorCommand(iss.Dir)` に変異させても
  全テストが green だった（ユーザーにはディレクトリが開き、issue は開かない）
- 現実的な変異形の `iss.Rel`（CWD 相対の存在しないパス）でも同じく green

対象を検証しているのは `stubEditorCapture` を使うテストだけで、エディタ連携キーは
**viewer の `e`（別名 `v`）/ git log 一覧の `e`（repo root）/ job パネルの `v`（stdin の scratch）**
と 3 系統あるため、「どのキーが何を開くか」の取り違えが構造的に検知できない箇所が残る。

（`e` の対象検証は commit `132cc81` で 1 本だけ塞いだが、ヘルパー側は直していない。
そのときインライン stub を書きかけて、既に `stubEditorCapture` があることに後から気づいた
= 系統が散っていること自体が発見を妨げた実例。）

## 対応案

1. `stubEditor` を廃し、`stubEditorCapture` に一本化する（回数は `len(*cmds)` で足りる）
2. 直書き 5 箇所を `stubEditorCapture` へ寄せる。特殊な戻り値（`editorClosedMsg{err}` を返して
   失敗経路を作る等）が要る箇所だけ、理由をコメントに書いて残す
3. ヘルパーの置き場所を決める。今は `open_workspace_test.go`（repo root を開くキーのテスト）に
   あるが、3 系統のキーが共有するので `tui_helpers_test.go` が妥当

## 受け入れ条件

- `stubEditor` の定義と利用が消えている
- エディタを起動する全キーのテストが「**何を開いたか**」（`cmd.Args`）まで assert している
- 変異検証: `editorCommand(iss.Path)` → `iss.Dir` / `iss.Rel` の変異で red になる。
  job パネルの stdin 経路・repo root の `nvim .` も同様に対象の取り違えで red になる

## 効能（誇張しない範囲で）

「何を開いたか」の検証が 1 つの作法に揃うので、次にエディタ連携キーを足すとき対象の検証が
既定で付いてくる。`runEditorCmd` の契約（戻り値・エラーの扱い）が変わったときに直す箇所も
1 箇所になる。⚠️ 一方でこれは**テスト側の整理**であり、production の複雑性は下がらない。
