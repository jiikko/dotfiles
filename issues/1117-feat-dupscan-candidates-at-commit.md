# 1117 (feat): 新しい関数の候補 (dupscan) を commit の時点でも出し、Bash で関数を足す経路を塞ぐ

起票日: 2026-11-27

## 概要

新しい関数を書く直前に既存の候補を出す hook (`inject-dupscan-candidates-on-new-func.sh`) は Write / Edit にしか掛からず、Bash (python / sed) で足した関数を見ない。本文は反証レビューの後に入れる。
