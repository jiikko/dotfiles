// 背景タスクの完了を待ったまま動かないセッションを知らせる。
//
// 背景タスクの表は tool 呼び出しの結果 (起動) と、task-notification のプロンプト / subagent の turn.complete (完了) から自分で作る。
// classic.Stop の background_tasks を読まないのは、managed settings のあるマシンでは cc-plugin-sec-default が classic.* を mod へ
// 届けないから (2.1.322 で実測。docs/claude-mods.md の「届かないイベント」)。届かない口に頼ると、知らせが何も出ないまま止まる。
// 表は $.state に置く: hot reload (/config で options を変えたときも) で module の変数が消えても、待っているタスクを忘れない。
import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import { EMPTY, alertText, alertsFor, due, finishedIds, keyOf, launched, statusText, wakePrompt } from './watch'
import type { Watch } from '../types'

const watch = atom({ plugin: 'bg-watchdog', key: 'watch' } as const, EMPTY as Watch)

const CHECK_EVERY_MS = 60_000

// 見張りの hook が落ちても、セッションの本来の動き (プロンプト・ターン・tool) は止めない。
// 失敗した hook の next は再生できる (呼んだ後なら同じ結果を返し、下の hook を 2 回走らせない) ので、どちらでも next(e) でよい
const passOn = (_$: unknown, e: never, next: (e: never) => unknown) => next(e)

// 動きがあった: 時刻を進め、出していた知らせを消す (agent が動いた = もう止まっていない)
async function touch($: EngineInterface, change: (w: Watch) => Watch): Promise<void> {
  const nowMs = await $.clock.now()
  let cleared = false
  await update($, watch, w => {
    const cur = change(w ?? EMPTY)
    cleared = cur.alertedAt !== null
    return { ...cur, lastActivityAt: nowMs, alertedAt: null }
  })
  if (cleared) $.ui.status(undefined)
}

const same = (w: Watch) => w

// 終わった背景 agent を表から落とす。agent の終わりは通知と turn.complete でも拾うが、名前で TaskStop した agent は
// 表の id (agentId) と食い違うので、engine の名簿 (終わった agent は数秒後に消える) とも突き合わせる
const ENDED = new Set(['completed', 'failed', 'killed'])

// 持ち主本人が送ったプロンプトの入口 (型定義の PromptOrigin の説明で選んだ): 端末・Remote Control (スマホ・web)・Slack からの呼びかけ。
// wake (plugin)・通知・他のセッションからのものでは数え直さない (数え直すと上限が効かない)。channel は MCP の中継で bot の投稿でも
// 届くので入れない (CI の通知が続くと上限が効かなくなる)。auto-continuation は人の操作なしに出る場面を確かめていないので入れない
const HUMAN = new Set(['composer', 'bridge', 'slack-ping'])
async function liveAgents($: EngineInterface): Promise<Set<string> | null> {
  try {
    const list = await $.agent.list()
    return new Set(list.filter(a => !ENDED.has(a.status)).map(a => a.id))
  } catch {
    return null // 名簿が読めない: agent を落とさない (落とす側の誤りは見張りを黙らせる)
  }
}

async function check($: EngineInterface, thresholdMs: number, wake: boolean): Promise<void> {
  const nowMs = await $.clock.now()
  const live = await liveAgents($)
  let fire: Watch | null = null
  // 判定と記録を 1 回の update の中で行う (重なった周が同じ知らせを 2 回出さない)。
  // update は書き込みが外れると callback をやり直すので、fire は毎回まず空に戻す (前の試行の判定を持ち越さない)
  await update($, watch, w => {
    fire = null
    let cur = w ?? EMPTY
    if (live !== null) {
      // 名簿を読んだ後に起動した agent は、古い名簿に載っていないだけなので落とさない
      const stale = Object.values(cur.tasks).filter(t => t.kind === 'agent' && t.startedAt < nowMs && !live.has(t.id))
      if (stale.length > 0) {
        const tasks = { ...cur.tasks }
        for (const t of stale) delete tasks[t.id]
        cur = { ...cur, tasks }
      }
    }
    if (!due(cur, nowMs, thresholdMs)) return cur
    fire = cur
    return { ...cur, alertedAt: nowMs, alerts: alertsFor(cur) + 1, alertKey: keyOf(cur.tasks) }
  })
  const w = fire as Watch | null
  if (w === null) return
  const text = alertText(w, nowMs)
  $.ui.toast(text, { timeoutMs: 15_000 })
  $.ui.status(statusText(w, nowMs))
  // 記録してから status を出すまでの間に動きがあると、touch は消す前の status を見ずに通り過ぎる。出した後に読み直して消す
  const after = (await read($, watch)) ?? EMPTY
  if (after.alertedAt !== nowMs) $.ui.status(undefined)
  // tmux のペイン表示・ベル・macOS の通知は settings の hook と同じ script に任せる (見えていないペインのときだけ鳴らす判定もそちら)
  try {
    await $.process.run([`${$.plugin.root}/../../hooks/tmux-pane-state.sh`, 'bg-stuck', text], { timeoutMs: 10_000 })
  } catch {
    // script が起動できない: toast と status line だけで知らせる
  }
  if (wake) void $.prompt.submit({ text: wakePrompt(w, nowMs) })
}

export const register: Register = (on, options) => {
  const minutesOpt = typeof options.thresholdMinutes === 'number' && options.thresholdMinutes > 0 ? options.thresholdMinutes : 20
  const thresholdMs = minutesOpt * 60_000
  const wake = options.wake === true

  on('session.start', async ($, e, next) => {
    const r = await next(e)
    await touch($, same)
    // hot reload では古い timer は engine が止めるので、張り直してよい
    $.clock.every(CHECK_EVERY_MS, () => {
      void check($, thresholdMs, wake).catch(() => {
        // $ の口が拒否されても timer を未処理の reject にしない (次の周でやり直す)
      })
    })
    return r
  }).catch(passOn)

  on('prompt.submit', async ($, e, next) => {
    const done = e.origin.kind === 'task-notification' ? finishedIds(e.text) : []
    const human = HUMAN.has(e.origin.kind)
    await touch($, w => {
      const tasks = { ...w.tasks }
      for (const id of done) delete tasks[id]
      return { ...w, tasks, alerts: human ? 0 : w.alerts } // 人が来た: 次に止まったら、また上限まで知らせる
    })
    return next(e)
  }).catch(passOn)

  // 未確認のリスク: メインのターンの途中で hot reload が起き、作り直した module が turn.complete を聞かないと、busy が true のまま残って
  // 以後は知らせない (型定義は「start を聞いた hook は end も聞く」としか言わない)。再現したら session.start で busy を見直す
  on('turn.start', async ($, e, next) => {
    await touch($, e.agentId === undefined ? w => ({ ...w, busy: true }) : same)
    return next(e)
  }).catch(passOn)

  on('turn.complete', async ($, e, next) => {
    const r = await next(e)
    const agentId = e.agentId
    await touch($, w => {
      if (agentId === undefined) return { ...w, busy: false }
      if (!(agentId in w.tasks)) return w
      const tasks = { ...w.tasks }
      delete tasks[agentId] // 背景の subagent は、自分のターンの終わりで報告を返して終わる
      return { ...w, tasks }
    })
    return r
  }).catch(passOn)

  on('tool.call', async ($, e, next) => {
    await touch($, same)
    const r = await next(e)
    const nowMs = await $.clock.now()
    const input = e as unknown as Record<string, unknown>
    const task = launched(e.tool, input, (r as { result?: unknown }).result, nowMs)
    const stopId = e.tool === 'TaskStop' ? (input.task_id ?? input.shell_id) : undefined
    // 外すのは成功した TaskStop だけ。失敗 (isError) と拒否 ({ deny }) では、まだ走っているかもしれないので残す。
    // 失敗の文言から「もう終わっていた」を読み分けない: 人が書いた拒否の理由や hook の stderr の語にも当たる。
    // 終わったタスクが残っても、知らせは同じ組に MAX_ALERTS 回で止まる
    const failed = (r as { isError?: boolean }).isError === true || 'deny' in (r as object)
    const stopped = typeof stopId === 'string' && !failed ? stopId : undefined
    if (task !== null || stopped !== undefined) {
      await touch($, w => {
        const tasks = { ...w.tasks }
        if (task !== null) tasks[task.id] = task
        if (stopped !== undefined) delete tasks[stopped]
        return { ...w, tasks }
      })
    }
    return r
  }).catch(passOn)
}
