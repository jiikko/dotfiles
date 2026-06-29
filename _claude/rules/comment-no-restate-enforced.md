# 実装で強制されていることを、改めてコメントで表明しない

## ルール

- **lint / 型 / コンパイラ / exhaustive switch などの「実装」がすでに強制している不変条件を、コメントで再掲しない**。コードが真の出典であり、同じことをコメントに書くと二重管理になり、片方の更新漏れで乖離する
- コメントに残すのは **「実装では強制できない制約」だけ** にする。典型: なぜこの構造を素朴に作り替えないか (= メタな設計判断)、外部登録値との一致要求、過去事故の再現条件など。これらは [`pending-issue-rationale-in-code.md`](pending-issue-rationale-in-code.md) が言う「Why」に相当する
- 強制済みかどうかの判定: 「その不変条件を破る変更を入れたとき、**コメントを読まなくても** lint error / compile error / test 失敗で止まるか?」 → 止まるなら実装強制済み = コメント不要。止まらない (= 人がコメントを読んで初めて気づく) ならコメントで残す価値がある

## 強制済み (コメント不要) と コメントで残す の例

| 内容 | 強制手段 | コメントに書くか |
|---|---|---|
| 「provider 固有の振る舞いを `service == .case` で分岐するな」 | SwiftLint custom rule (error) | ❌ 書かない (lint が止める) |
| 「新 case を全 callsite で処理せよ」 | exhaustive switch (`default:` なし) で compile error | ❌ 書かない (compiler が止める) |
| 「この値は non-nil」 | 型 (Optional でない) | ❌ 書かない |
| 「この enum を registry/plugin に作り替えるな (網羅性が消え silent gap が runtime に戻る)」 | **強制不可** (switch を消す改修自体を compiler は禁止できない) | ✅ 書く |
| 「この port は外部 IdP の登録値と一致が必要」 | **強制不可** | ✅ 書く |

起源 (obaket 357, 2026-06-29): `StorageServiceKind` enum に「振る舞いは protocol 側でポリモーフィズム済 (= `service ==` 分岐は lint で禁止済)」と書きかけたが、それは SwiftLint `presentation_no_provider_specific_branch` と exhaustive switch が既に強制している事実だった。再掲を削り、実装で守れない「registry 化するな」の rationale だけ残した。

## 既存コメントを触ったときの掃除

- コードを touch した際、近傍コメントが「今は実装強制されている不変条件の再掲」になっていたら、その場で削除する (lint ルール追加・型変更・exhaustive 化などで後から強制が効くようになり、コメントが冗長化しているパターン)
- 強制手段を**新設したとき** (custom lint rule / 型強化 / test 追加) は、それと重複する既存コメントが無いか確認して削除する

## やること / やらないこと

- ✓ コメントは「実装で強制できない制約 (Why / メタ設計判断)」に限定する
- ✓ 強制済みかは「コメント無しで lint/compile/test が止めるか」で判定する
- ✓ 強制手段を新設したら重複コメントを削除する
- ✗ lint / 型 / exhaustive switch が守っている不変条件をコメントで再掲する
- ✗ 「念のため」で実装強制済みの規約をコメントにも書く (二重管理 = 乖離の温床)

## 関連

- [`claude-md-maintenance.md`](claude-md-maintenance.md) — 「What はコードが真の出典、ドキュメントは Why を保存する」同思想 (本ルールはコメント版)
- [`pending-issue-rationale-in-code.md`](pending-issue-rationale-in-code.md) — コメントに残すべき「実装で守れない制約」の書き方
- [`verify-design-intent-before-refactor.md`](verify-design-intent-before-refactor.md) — 「分割しないと判断した理由をコメントで残す」= 本ルールの ✅ 側 (強制不可な設計判断)
