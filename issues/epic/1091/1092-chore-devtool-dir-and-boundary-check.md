# 1092 (chore): devtool/ の置き場・PATH・外を参照しない境界の検査を作る

起票日: 2026-11-19

## 概要

devtool/ を作り、PATH に足し、devtool/ の中から devtool/ の外をパスで指したら make test で落とす。
