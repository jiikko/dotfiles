export type IssueBandCounts = {
  overdue: number
  soon: number
  broken: number
  retro: number
  error: string | null
}

declare module 'claude-code' {
  interface PluginState {
    'issue-band': { counts: IssueBandCounts | null }
  }
}
