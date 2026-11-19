# 1096 (bug): test_warn_invisible_chars_in_tool_input.sh の 150 万行のケースが make test の負荷の下で時間切れになる

起票日: 2026-11-19

## 概要

壁時計 10 秒の上限で hook の所要を判定しているため、並列の make test の負荷の下で rc=124 になる (単独では 0.15 秒で緑)。
