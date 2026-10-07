// 5h 枠の判定 (純粋関数)。閾値・鮮度の境界は bin/ratelimit (src/ratelimit) と揃える:
//   THRESHOLD      = `ratelimit -warn-5h` の既定値 80、比較は `>=` (src/ratelimit/main.go の overLimit)
//   LIVE_MAX_AGE   = main.go の maxStale (30 分。古い % で判定し続けると、実際は超過していても黙る)
//   FILE_MAX_AGE   = src/ratelimit/usage/statusline.go の statuslineMaxAge (15 分)
// 揃えるべき値がそちらで変わったら、ここも変える (機械では突き合わせていない)。
import type { Reading } from '../types'

export const THRESHOLD = 80
export const LIVE_MAX_AGE_MS = 30 * 60_000
export const FILE_MAX_AGE_MS = 15 * 60_000
export const ENV_MARK = 'DOTFILES_MOD_RATELIMIT_WARN'

type RateLimit = { kind: string; percentUsed: number; resetsAt?: string }

/** $.session.usage().rateLimits (直近の応答が報告した値) から 5h 枠を取る。観測時刻は呼び出し側の近似 (measure の発火時刻)。 */
export function fromLive(limits: readonly RateLimit[], observedAtMs: number): Reading | null {
  const w = limits.find(l => l.kind === 'five_hour')
  if (w === undefined) return null
  if (w.resetsAt === undefined) return null // 使用率があるのにリセット時刻が無い形は使わない (statusline reader と同じ)
  const resetsAtMs = Date.parse(w.resetsAt)
  if (Number.isNaN(resetsAtMs)) return null
  // 切り捨て: statusline (write_rate_limits) も小数を切り捨てて int にするので、同じ観測が live と file で違う % にならないようにする
  return { percent: Math.floor(w.percentUsed), resetsAtMs, observedAtMs, source: 'live' }
}

/** statusline が書く claude-rate-limits.json (src/ratelimit/usage/statusline.go の statuslineState と 1:1)。使えなければ null。 */
export function parseFile(text: string, nowMs: number): Reading | null {
  let st: { observedAt?: unknown; five_hour?: { used_percentage?: unknown; resets_at?: unknown } | null }
  try {
    st = JSON.parse(text)
  } catch {
    return null
  }
  if (typeof st.observedAt !== 'number' || st.observedAt <= 0) return null
  const observedAtMs = st.observedAt * 1000
  if (observedAtMs > nowMs || nowMs - observedAtMs >= FILE_MAX_AGE_MS) return null
  const w = st.five_hour
  if (w === null || w === undefined || w.used_percentage === null || w.used_percentage === undefined) {
    // 窓がリセットを過ぎて落とされた: 空から始まっている
    return { percent: 0, resetsAtMs: null, observedAtMs, source: 'file' }
  }
  if (typeof w.used_percentage !== 'number') return null
  if (typeof w.resets_at !== 'number') return null // 使用率はあるのにリセット時刻が無い: 推測せず使わない
  return { percent: Math.floor(w.used_percentage), resetsAtMs: w.resets_at * 1000, observedAtMs, source: 'file' }
}

export function isUsable(r: Reading, nowMs: number): boolean {
  if (r.observedAtMs > nowMs) return false
  const maxAge = r.source === 'live' ? LIVE_MAX_AGE_MS : FILE_MAX_AGE_MS
  return nowMs - r.observedAtMs < maxAge
}

/** 使える観測のうち、観測時刻が新しい方。どちらも使えなければ null (= 判定できない)。 */
export function pick(live: Reading | null, file: Reading | null, nowMs: number): Reading | null {
  const candidates = [live, file].filter((r): r is Reading => r !== null && isUsable(r, nowMs))
  if (candidates.length === 0) return null
  return candidates.reduce((a, b) => (b.observedAtMs > a.observedAtMs ? b : a))
}

export type Verdict = { over: boolean; percent: number; resetsAtMs: number | null }

export function judge(r: Reading, nowMs: number): Verdict {
  if (r.resetsAtMs !== null && r.resetsAtMs <= nowMs) {
    // 観測の後にリセットを過ぎた: 窓は空から始まっている
    return { over: false, percent: 0, resetsAtMs: r.resetsAtMs }
  }
  return { over: r.percent >= THRESHOLD, percent: r.percent, resetsAtMs: r.resetsAtMs }
}

const p2 = (n: number) => String(n).padStart(2, '0')

/** main.go の formatReset と同じ: 今日なら HH:MM、それ以外は M/D HH:MM (ローカル時刻) */
export function formatReset(resetMs: number, nowMs: number): string {
  const t = new Date(resetMs)
  const now = new Date(nowMs)
  const hm = `${p2(t.getHours())}:${p2(t.getMinutes())}`
  const sameDay = t.getFullYear() === now.getFullYear() && t.getMonth() === now.getMonth() && t.getDate() === now.getDate()
  return sameDay ? hm : `${t.getMonth() + 1}/${t.getDate()} ${hm}`
}

/** 注入する文。_claude/hooks/ratelimit-warn.sh の printf と同じ 3 行 (_claude/rules/subagent-model-tiering.md が名指しする文面) */
export function message(v: Verdict, nowMs: number): string {
  const reset = v.resetsAtMs === null ? '不明' : formatReset(v.resetsAtMs, nowMs)
  return [
    '🚨 Claude の 5h 枠が閾値を超えている:',
    `claude 5h ${v.percent}% (${reset} にリセット)`,
    '大きな作業に入る前に、控える・縮小する・リセット後に回す案をユーザーへ提案すること (基準は subagent-model-tiering.md の「枠の残量」)。',
  ].join('\n')
}

export function statusText(v: Verdict): string | undefined {
  return v.over ? `🚨 5h ${v.percent}%` : undefined
}
