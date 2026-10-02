import type { DesktopStatuslineSegment as Segment } from '../types'

// statusline-command.sh が使う SGR だけを desktop の文字の属性へ置き換える (色の閾値は script が決める。ここは写さない)。
// 色は端末の 16 色を hex にした値 (desktop は CSS の色を受ける。名前の色は明暗の差が大きいので使わない)
const FG: Record<number, string> = {
  30: '#2d333b', 31: '#e5534b', 32: '#57ab5a', 33: '#c69026', 34: '#539bf5', 35: '#b083f0', 36: '#39c5cf', 37: '#cdd9e5',
  90: '#768390', 91: '#f47067', 92: '#6bc46d', 93: '#daaa3f', 94: '#6cb6ff', 95: '#dcbdfb', 96: '#56d4dd', 97: '#ffffff',
}
const BG: Record<number, string> = {
  40: '#2d333b', 41: '#e5534b', 42: '#57ab5a', 43: '#c69026', 44: '#539bf5', 45: '#b083f0', 46: '#39c5cf', 47: '#cdd9e5',
}

type Style = Omit<Segment, 'text'>

const apply = (style: Style, codes: number[]): Style => {
  let s: Style = { ...style }
  if (codes.length === 0) codes = [0]
  for (const c of codes) {
    if (c === 0) s = {}
    else if (c === 1) s.bold = true
    else if (c === 4) s.underline = true
    else if (c === 22) delete s.bold
    else if (c === 24) delete s.underline
    else if (c === 39) delete s.color
    else if (c === 49) delete s.backgroundColor
    else if (FG[c]) s.color = FG[c]
    else if (BG[c]) s.backgroundColor = BG[c]
    // 5 (点滅) などは desktop では表せないので捨てる
  }
  return s
}

// parseAnsi: script の出力を行ごとの区切り (同じ属性の連続) に分ける。空の区切りは作らない
export const parseAnsi = (out: string): Segment[][] => {
  const lines: Segment[][] = []
  let style: Style = {}
  for (const raw of out.replace(/\n+$/, '').split('\n')) {
    const line: Segment[] = []
    const re = /\x1b\[([0-9;]*)m/g
    let last = 0
    let m: RegExpExecArray | null
    const push = (text: string) => {
      if (text !== '') line.push({ text, ...style })
    }
    while ((m = re.exec(raw)) !== null) {
      push(raw.slice(last, m.index))
      style = apply(style, m[1] === '' ? [] : m[1].split(';').map(Number))
      last = re.lastIndex
    }
    push(raw.slice(last))
    lines.push(line)
  }
  return lines
}
