# 1101 (feat): tmpclean と cleanup-tmp-dir で ~/Library/Preferences の UUID 入りの plist の残骸を数え、選んだ形だけをゴミ箱へ移す

起票日: 2026-11-20

## 概要

テストが `UserDefaults(suiteName:)` などで作った、名前に UUID を含む plist が `~/Library/Preferences` に溜まる。それを形ごとに数え、ユーザーが選んだ形だけを `tmpclean` でゴミ箱へ移せるようにする。
