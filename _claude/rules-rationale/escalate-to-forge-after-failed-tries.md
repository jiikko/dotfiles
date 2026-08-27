# バグ修正で 1-2 回の自前試行が失敗したら、迷わず forge skill Maximum に escalate — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/escalate-to-forge-after-failed-tries.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ (起源: DualNote iOS #030 IME バグ, 2026-05-23)

「delegate cycle」という hypothesis に固執して 3 回試行 (フラグ抑制 → delegate=nil → 未検証の overclaim docstring) して全て外した。forge Maximum を起動したら複数の専門家エージェントが全員一致で**別の真因**を特定し 1 セッションで構造的解決。損失は 3-4 時間の無駄な試行と「治っていない」報告 2 回。forge 1 回 ~30 分のコストの方が明らかに安い。
