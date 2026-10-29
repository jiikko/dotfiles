# 974 (bug): L1 が局所の名前と同じ綴りの外側の名前も番号にし、違う package・プロパティを使う 2 つを同じ形にする

起票日: 2026-10-29

概要: norm の局所の名前の表が scope を見ず綴りで照合するので、`bytes := bytes.TrimSpace(b)` と `strings := strings.TrimSpace(s)`、Swift の `if let user` と `if let account` が L1 の組になる。
