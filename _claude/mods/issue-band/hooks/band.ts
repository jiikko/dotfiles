import type { IssueBandCounts } from '../types'

// script の --counts の出力 (`key=value` を 1 行ずつ) を読む。数えるのは script (_claude/hooks/human-tasks-due.sh /
// retro-open.sh) で、ここは写さない (issue 621)。どちらも何も出さない = issue dir の無い repo なので null
export const parseCounts = (human: string, retro: string): IssueBandCounts | null => {
  if (human.trim() === '' && retro.trim() === '') return null
  const kv = new Map<string, string>()
  for (const line of `${human}\n${retro}`.split('\n')) {
    const i = line.indexOf('=')
    if (i > 0) kv.set(line.slice(0, i), line.slice(i + 1))
  }
  const num = (k: string) => {
    const v = Number(kv.get(k) ?? '0')
    return Number.isInteger(v) && v >= 0 ? v : 0
  }
  return { overdue: num('overdue'), soon: num('soon'), broken: num('broken'), retro: num('retro'), error: kv.get('error') ?? null }
}

export type Segment = { text: string; color: 'red' | 'yellow' | 'cyan' }

// 案 3 (2026-10-02 にユーザーが選んだ): 人がやる必要があるときだけ出す。余裕のある human だけなら何も出さない (空配列)
export const segments = (c: IssueBandCounts): Segment[] => {
  const out: Segment[] = []
  if (c.error) out.push({ text: `issue の催促を数えられない: ${c.error}`, color: 'red' })
  if (c.overdue > 0) out.push({ text: `期限切れの human ${c.overdue} 件`, color: 'red' })
  if (c.soon > 0) out.push({ text: `期限が近い human ${c.soon} 件`, color: 'yellow' })
  if (c.broken > 0) out.push({ text: `期限の読めない human ${c.broken} 件`, color: 'yellow' })
  if (c.retro > 0) out.push({ text: `retro 未決着 ${c.retro}`, color: 'cyan' })
  return out
}
