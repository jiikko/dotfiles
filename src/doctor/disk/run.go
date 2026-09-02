package disk

import "time"

// cmdTimeout は補助コマンド (simctl / brew / pgrep) 1 回の上限。
const cmdTimeout = 30 * time.Second
