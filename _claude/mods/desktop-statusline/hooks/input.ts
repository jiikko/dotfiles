// CLI の statusLine が script に渡す JSON と同じ形を、mod から取れる値で組む。script が読むのは
// workspace.current_dir / model.display_name / rate_limits.{five_hour,seven_day}.{used_percentage,resets_at (エポック秒)} /
// context_window.{total_input_tokens,context_window_size,used_percentage} / effort.level / transcript_path / session_id (_claude/statusline-command.sh)
export type UsageLike = {
  context: { tokens?: number; window: number; percent?: number }
  rateLimits: readonly { kind: string; percentUsed: number; resetsAt?: string }[]
}

// claude-opus-5-5 → Opus 5.5 (CLI の display_name の形)。読めない id はそのまま
export const displayName = (id: string): string => {
  const m = /^claude-([a-z]+)-(\d+)(?:-(\d+))?/.exec(id)
  if (!m) return id
  const family = m[1]!.charAt(0).toUpperCase() + m[1]!.slice(1)
  return `${family} ${m[2]}${m[3] ? `.${m[3]}` : ''}`
}

const window = (u: UsageLike, kind: string) => {
  const w = u.rateLimits.find(r => r.kind === kind)
  if (!w) return undefined
  const at = w.resetsAt ? Math.floor(Date.parse(w.resetsAt) / 1000) : undefined
  return { used_percentage: w.percentUsed, ...(at !== undefined && !Number.isNaN(at) ? { resets_at: at } : {}) }
}

export const statusInput = (a: { cwd: string; modelId: string; usage: UsageLike; effort?: string; sessionId: string }) => ({
  cwd: a.cwd,
  workspace: { current_dir: a.cwd },
  model: { display_name: displayName(a.modelId) },
  rate_limits: { five_hour: window(a.usage, 'five_hour'), seven_day: window(a.usage, 'seven_day') },
  context_window: {
    total_input_tokens: a.usage.context.tokens,
    context_window_size: a.usage.context.window,
    used_percentage: a.usage.context.percent,
  },
  ...(a.effort ? { effort: { level: a.effort } } : {}),
  session_id: a.sessionId,
  transcript_path: '',
})
