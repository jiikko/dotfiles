// 背景タスクの表と「知らせる時か」の判定・文面。$ に触らない純粋関数だけを置く (register.ts が配線する)。
import type { Task, Watch } from '../types'

export const EMPTY: Watch = { tasks: {}, busy: false, lastActivityAt: 0, alertedAt: null, alerts: 0, alertKey: '' }

// 同じタスクの組に知らせる回数の上限。表に終わったタスクが残ったとき (通知の取りこぼし・意図して長く走らせる dev server)、
// 上限が無いと人のいない夜の間ずっと鳴り続け、wake なら 20 分ごとに 1 ターンずつ使い続ける。
// 組が変わる (タスクが増える・減る) か、人がプロンプトを打つと数え直す
export const MAX_ALERTS = 3

export const keyOf = (tasks: Record<string, Task>) => Object.keys(tasks).sort().join(',')

/** 同じ組に何回知らせたか (組が変わっていれば 0) */
export const alertsFor = (w: Watch) => (w.alertKey === keyOf(w.tasks) ? w.alerts : 0)

// 通知の <status> のうち、タスクが終わったもの。これ以外 (Monitor の途中の出来事・不明な値) では表から消さない
// (消すと、まだ走っている背景タスクを見張りから外す。残す側の誤りは、次の通知か agent の確認で直る)
const TERMINAL = new Set(['completed', 'failed', 'killed', 'stopped', 'cancelled', 'error'])

const str = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined)

const oneLine = (s: string) => s.replace(/\s+/g, ' ').trim()

/**
 * tool 呼び出しの結果から、起動した背景タスクを読む。起動していなければ null。
 * 形は 2.1.322 の実測: Bash の run_in_background は result.backgroundTaskId、Agent は result.status === 'async_launched' と result.agentId。
 * 上限を超えて背景へ回された Bash と、Monitor / Workflow は実測していないので、同じ欄名を読むだけにする
 */
export function launched(tool: string, input: Record<string, unknown>, result: unknown, nowMs: number): Task | null {
  if (result === null || typeof result !== 'object') return null
  const r = result as Record<string, unknown>
  const label = oneLine(str(input.description) ?? str(input.command) ?? str(input.prompt) ?? tool)
  const bgId = str(r.backgroundTaskId)
  if (bgId !== undefined) return { id: bgId, kind: tool === 'Bash' ? 'shell' : 'other', label: `${tool}: ${label}`, startedAt: nowMs }
  const agentId = str(r.agentId)
  if (tool === 'Agent' && r.status === 'async_launched' && agentId !== undefined) {
    return { id: agentId, kind: 'agent', label: `Agent: ${label}`, startedAt: nowMs }
  }
  if (tool !== 'Bash' && tool !== 'Agent') {
    // TaskCreate などの「タスク一覧」の taskId を拾わないよう、背景で走る道具だけに限る
    const taskId = str(r.taskId)
    if ((tool === 'Monitor' || tool === 'Workflow') && taskId !== undefined) {
      return { id: taskId, kind: 'other', label: `${tool}: ${label}`, startedAt: nowMs }
    }
  }
  return null
}

/** 通知の本文 (<task-notification> の並び) から、終わったタスクの id を全部読む */
export function finishedIds(text: string): string[] {
  const ids: string[] = []
  for (const block of text.match(/<task-notification>[\s\S]*?<\/task-notification>/g) ?? []) {
    const id = block.match(/<task-id>\s*([^<\s]+)\s*<\/task-id>/)?.[1]
    const status = block.match(/<status>\s*([^<\s]+)\s*<\/status>/)?.[1]
    if (id !== undefined && status !== undefined && TERMINAL.has(status)) ids.push(id)
  }
  return ids
}

/** 知らせる時か: 見張る背景タスクがあり、メインのターンが止まっていて、最後の動きからも最後の知らせからも閾値を過ぎた */
export function due(w: Watch, nowMs: number, thresholdMs: number): boolean {
  if (Object.keys(w.tasks).length === 0 || w.busy) return false
  if (nowMs - w.lastActivityAt < thresholdMs) return false
  if (alertsFor(w) >= MAX_ALERTS) return false
  return w.alertedAt === null || nowMs - w.alertedAt >= thresholdMs
}

export const minutes = (ms: number) => Math.max(0, Math.floor(ms / 60_000))

function listed(w: Watch, nowMs: number, max: number): string {
  const tasks = Object.values(w.tasks).sort((a, b) => a.startedAt - b.startedAt)
  const shown = tasks.slice(0, max).map(t => `${t.label} (起動から ${minutes(nowMs - t.startedAt)} 分)`)
  const rest = tasks.length - shown.length
  return rest > 0 ? `${shown.join('、')} ほか ${rest} 件` : shown.join('、')
}

/** w は知らせる直前の値 (alerts はまだ増やしていない) */
export function alertText(w: Watch, nowMs: number): string {
  const last = alertsFor(w) + 1 >= MAX_ALERTS ? ' (この組では最後の知らせ。プロンプトを打つか、背景タスクが増減すると、また知らせる)' : ''
  return `🚨 背景タスクの完了待ちのまま ${minutes(nowMs - w.lastActivityAt)} 分動きがない: ${listed(w, nowMs, 2)}${last}`
}

export function statusText(w: Watch, nowMs: number): string {
  return `🚨 背景待ち ${minutes(nowMs - w.lastActivityAt)} 分`
}

export function wakePrompt(w: Watch, nowMs: number): string {
  const tasks = Object.values(w.tasks)
    .sort((a, b) => a.startedAt - b.startedAt)
    .map(t => `- ${t.kind} ${t.id}: ${t.label} (起動から ${minutes(nowMs - t.startedAt)} 分)`)
  return [
    `[bg-watchdog] 背景タスクの完了を待ったまま ${minutes(nowMs - w.lastActivityAt)} 分、このセッションに動きがない。完了の通知をまだ受けていない背景タスク:`,
    ...tasks,
    'それぞれの出力ファイルと状態を確かめ、止まっている・失敗しているものは理由を調べて止め、ユーザーに報告すること。',
    '正しく走り続けているだけなら、その根拠 (出力が増えている等) を 1 行で書いて待ちに戻ってよい。',
  ].join('\n')
}
