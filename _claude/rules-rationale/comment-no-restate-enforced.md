# 実装で強制されていることを、改めてコメントで表明しない — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/comment-no-restate-enforced.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## ルール本文から移した実例

本文には規範だけを残し、その根拠になった実例をここへ移した（元の文脈のまま）。

起源 (obaket 357, 2026-06-29): `StorageServiceKind` enum に「振る舞いは protocol 側でポリモーフィズム済 (= `service ==` 分岐は lint で禁止済)」と書きかけたが、それは SwiftLint `presentation_no_provider_specific_branch` と exhaustive switch が既に強制している事実だった。再掲を削り、実装で守れない「registry 化するな」の rationale だけ残した。
