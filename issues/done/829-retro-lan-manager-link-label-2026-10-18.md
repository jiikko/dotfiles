# 829 (retro): lan-manager の (有線) / (無線) の表示 (issue 827 / 828) の振り返り

起票日: 2026-10-18

## どこで踏んだか

1. 既存テストの偽物 (`lanmgr/model_test.go` の `selfNet()`) のサービスに Hardware Port が無く、Port で決める新しい判定が全テストで Wi-Fi を「有線」と読んでいた。
   本物の出力には必ず Port がある。偽物を本物の形に揃えて気づいた (テストが緑のまま誤った前提で回っていた)
2. 足したテスト 1 本 (繋ぐ先の線のメインへの落とし) が、直す前の実装でも緑だった。入力が別の分岐 (`lanAway`) に入り、直した分岐に届いていなかった。変異で緑を見て気づき、前提を assert で固定した
3. 表のセルの中で色を分けて描くと、選んだ行の地の色が途中で途切れた。テストは文字 (SGR を剥がした行) しか見ておらず、敵対的レビューの指摘を SGR のバイト列を出して確かめた

## 提案

- 1: 却下。`survey-receiver-guards-before-passing-new-values.md` (fake に受け側の挙動を模させる) と `measure-external-cli-streams-separately.md` (fake は本物で測ってから書く) で足りる。新しく読む欄を偽物が持っているかは、その 2 つの適用で拾える
- 2: 却下。`mutation-verify-new-tests.md` の手順どおりに変異で拾えた (規範は機能した)
- 3: 却下 (局所)。lipgloss の入れ子の描画に固有。この repo では `lanmgr/view.go` の `ipCell` のコメントと `TestSelectedRowKeepsIPAndTagTogether` に残した
