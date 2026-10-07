/** 5h 枠の 1 つの観測。resetsAtMs が null = 窓がリセットを過ぎて落とされた (0% で空から始まっている) */
export type Reading = {
  percent: number
  resetsAtMs: number | null
  observedAtMs: number
  source: 'live' | 'file'
}

declare module 'claude-code' {
  interface PluginState {
    'ratelimit-warn': { live: Reading | null }
  }
}
