# 1085 (bug): issue_claim.sh が push の「cannot lock ref」を再試行せずに失敗で終わる

起票日: 2026-11-13

概要: pre-push の gate を走らせている間に origin が進むと、push は non-fast-forward ではなく `cannot lock ref … expected` で弾かれ、`scripts/issue_claim.sh` は再試行せずに rc 1 で終わる (obaket retro 1487 の 1)。
