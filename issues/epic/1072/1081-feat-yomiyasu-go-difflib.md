# 1081 (feat): yomiyasu の diff の照合 (difflib と本家の速い照合) を Go で作り直す

起票日: 2026-11-13

1076 (diff) の一部を切り出したもの。Python の `difflib.SequenceMatcher` と本家の `_ExactSequenceMatcher` を `src/ja-writing/internal/difflib` に写し、本家と同じ答えを返すことを oracle の記録で確かめる。本文は反証レビューの後に書く。
