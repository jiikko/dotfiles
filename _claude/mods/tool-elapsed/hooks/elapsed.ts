// 帯に出す判定と文面 (純粋関数)。
import type { Run } from '../types'

/** これより短い呼び出しは帯に出さない (普通の Bash は数秒で終わる。出すのは「待っている」と感じる長さだけ) */
export const SHOW_AFTER_MS = 20_000
/** この間隔で toast を出す (1 本の呼び出しごとに 5 分・10 分・15 分 …) */
export const TOAST_EVERY_MS = 5 * 60_000

/** 帯の 1 行に使う名前。Bash は description があればそれ、無ければ command の先頭 */
export function labelOf(tool: string, input: unknown): string {
  if (tool === 'Bash' && input !== null && typeof input === 'object') {
    const i = input as { description?: unknown; command?: unknown }
    const d = typeof i.description === 'string' ? i.description.trim() : ''
    if (d !== '') return `Bash: ${d}`
    const c = typeof i.command === 'string' ? i.command.replace(/\s+/g, ' ').trim() : ''
    if (c !== '') return `Bash: ${c}`
  }
  if (tool === 'Agent' && input !== null && typeof input === 'object') {
    const i = input as { description?: unknown; subagent_type?: unknown }
    const d = typeof i.description === 'string' ? i.description.trim() : ''
    const t = typeof i.subagent_type === 'string' ? i.subagent_type : ''
    return `Agent${t !== '' ? ` (${t})` : ''}${d !== '' ? `: ${d}` : ''}`
  }
  return tool
}

/** 経過時間: mm:ss、1 時間を超えたら h:mm:ss */
export function formatElapsed(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000))
  const p2 = (n: number) => String(n).padStart(2, '0')
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  return h > 0 ? `${h}:${p2(m)}:${p2(sec)}` : `${p2(m)}:${p2(sec)}`
}

/** 帯に出す呼び出し: SHOW_AFTER_MS 以上走っているもの。長い順 */
export function visible(runs: Iterable<Run>, nowMs: number): Run[] {
  return [...runs]
    .filter(r => nowMs - r.startedAt >= SHOW_AFTER_MS)
    .sort((a, b) => a.startedAt - b.startedAt)
}

/** 次の 5 分の節目を越えていたら、その toast の文と、更新後の toasts。越えていなければ null */
export function dueToast(r: Run, nowMs: number): { text: string; toasts: number } | null {
  const next = r.toasts + 1
  if (nowMs - r.startedAt < next * TOAST_EVERY_MS) return null
  const minutes = Math.floor((nowMs - r.startedAt) / 60_000)
  return { text: `⏳ ${clip(r.label, 60)}: ${minutes} 分経過`, toasts: Math.floor((nowMs - r.startedAt) / TOAST_EVERY_MS) }
}

export const clip = (text: string, width: number) => (text.length > width ? `${text.slice(0, Math.max(0, width - 1))}…` : text)
