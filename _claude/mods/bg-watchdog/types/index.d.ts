/** 完了の通知をまだ受けていない背景タスク 1 つ。id は shell なら backgroundTaskId、agent なら agentId */
export type Task = { id: string; kind: 'shell' | 'agent' | 'other'; label: string; startedAt: number }

/**
 * busy: メインのターンが走っている (走っている間はターンの中の見張り = tool-elapsed の領分なので知らせない)。
 * lastActivityAt: 最後にプロンプト・ターン・tool 呼び出しが起きた時刻。alertedAt: この待ちの中で最後に知らせた時刻
 */
export type Watch = {
  tasks: Record<string, Task>
  busy: boolean
  lastActivityAt: number
  alertedAt: number | null
  /** alertKey (表の id の組) に対して何回知らせたか。組が変わるか、人がプロンプトを打つと数え直す */
  alerts: number
  alertKey: string
}

declare module 'claude-code' {
  interface PluginState {
    'bg-watchdog': { watch: Watch }
  }
}
