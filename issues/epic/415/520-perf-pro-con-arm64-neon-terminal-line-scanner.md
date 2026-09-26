# 520 (superseded): pro-con ARM64 / NEON terminal line scanner

この draft は、同じ番号・同じ目的で作られた
[`520-perf-arm64-terminal-line-scanner.md`](./520-perf-arm64-terminal-line-scanner.md)
へ統合した。

正本はそちら。pro-con だけでなく、共通の `src/tuikit/termwidth` と glogx まで含めて
「pure Go の 1-pass 化 → darwin/arm64 ASM/NEON → end-to-end benchmark」の順で検証する。
