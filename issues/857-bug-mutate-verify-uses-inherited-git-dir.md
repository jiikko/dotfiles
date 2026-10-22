# 857 (bug): mutate-verify が継承した GIT_DIR の repo を検証する

起票日: 2026-10-22

## 概要

継承した GIT_DIR が cwd と別の repo を指すと、mutate-verify は GIT_DIR 側の HEAD に cwd の差分を持ち込んだ混ざった木で判定する (本文は反証レビューの後に書く)。
