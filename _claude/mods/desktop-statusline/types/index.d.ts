export type DesktopStatuslineSegment = {
  text: string
  color?: string
  backgroundColor?: string
  bold?: boolean
  underline?: boolean
}

declare module 'claude-code' {
  interface PluginState {
    'desktop-statusline': { lines: DesktopStatuslineSegment[][] | null; error: string | null }
  }
}
