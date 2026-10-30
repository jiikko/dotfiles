# 実装で強制されていることを、改めてコメントで表明しない

## ルール

- **lint / 型 / コンパイラ / exhaustive switch などの「実装」がすでに強制している不変条件を、コメントで再掲しない**。コードが真の出典であり、同じことをコメントに書くと二重管理になり、片方の更新漏れで乖離する
- コメントに残すのは **「実装では強制できない制約」だけ** にする。典型: なぜこの構造を素朴に作り替えないか (= メタな設計判断)、外部登録値との一致要求、過去事故の再現条件など。これらは [`pending-issue-rationale-in-code.md`](pending-issue-rationale-in-code.md) が言う「Why」に相当する
- 強制済みかどうかの判定: 「その不変条件を破る変更を入れたとき、**コメントを読まなくても** lint error / compile error / test 失敗で止まるか?」 → 止まるなら実装強制済み = コメント不要。止まらない (= 人がコメントを読んで初めて気づく) ならコメントで残す価値がある

## なぜ

根拠・起源・実例は `~/dotfiles/_claude/rules-rationale/comment-no-restate-enforced.md` (起動時には読まれない。改訂するときに読む)。

## 強制済み (コメント不要) と コメントで残す の例

| 内容 | 強制手段 | コメントに書くか |
|---|---|---|
| 「provider 固有の振る舞いを `service == .case` で分岐するな」 | SwiftLint custom rule (error) | ❌ 書かない (lint が止める) |
| 「新 case を全 callsite で処理せよ」 | exhaustive switch (`default:` なし) で compile error | ❌ 書かない (compiler が止める) |
| 「この値は non-nil」 | 型 (Optional でない) | ❌ 書かない |
| 「この enum を registry/plugin に作り替えるな (網羅性が消え silent gap が runtime に戻る)」 | **強制不可** (switch を消す改修自体を compiler は禁止できない) | ✅ 書く |
| 「この port は外部 IdP の登録値と一致が必要」 | **強制不可** | ✅ 書く |

## 既存コメントを触ったときの掃除

- コードを touch した際、近傍コメントが「今は実装強制されている不変条件の再掲」になっていたら、その場で削除する (lint ルール追加・型変更・exhaustive 化などで後から強制が効くようになり、コメントが冗長化しているパターン)
- 強制手段を**新設したとき** (custom lint rule / 型強化 / test 追加) は、それと重複する既存コメントが無いか確認して削除する
