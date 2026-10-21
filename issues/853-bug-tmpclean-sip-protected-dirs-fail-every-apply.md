# 853 (bug): tmpclean が SIP で守られた $TMPDIR の dir を毎回候補に挙げ、apply が移せずに終了コード 1 になる

起票日: 2026-10-21

## 概要

`$TMPDIR` の直下の macOS のサービスの dir (`sunlnk` フラグと `com.apple.rootless` の xattr 付き) を tmpclean が候補に挙げ、apply が `operation not permitted` で移せない (本文は反証レビューの後に入れる)。
