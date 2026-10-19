# 847 (feat): lan-manager で管理対象の名前ごとに疎通を確かめる (名前解決 → ping → ssh の口)

起票日: 2026-10-20

## 概要

管理対象の相手の Bonjour 名 (`<host>.local`) と有線の名前 (`<host>.wired.local`) ごとに、名前解決 → ping → ssh (22 番) の口を確かめ、画面 (詳細カードのキー) と CLI (`mac-lan-manager ping`) で出す。本文は反証レビューの後に入れる。
