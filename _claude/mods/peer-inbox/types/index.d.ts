export type InboxItem = {
  dir: 'in' | 'out'
  who: string
  text: string
  at: number
  isRead: boolean
}

declare module 'claude-code' {
  interface PluginState {
    'peer-inbox': { items: InboxItem[]; isHidden: boolean }
  }
}
