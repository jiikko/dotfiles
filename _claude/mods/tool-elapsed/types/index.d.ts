/** 走っている tool 呼び出し 1 つ。toasts は「何回目の 5 分の toast まで出したか」 */
export type Run = { id: string; tool: string; label: string; startedAt: number; agentId?: string; toasts: number }

declare module 'claude-code' {
  interface PluginState {
    'tool-elapsed': { tick: number }
  }
}
