// Pure rendering of a pawl `--format json` verdict into the sticky PR-comment
// markdown body. Kept separate from the IO in pr-comment.mjs so the risky part
// — the table, status icons, and regression rendering — is unit-testable
// without GitHub. The status icons and the improved footer mirror pawl's own
// `--format text` output so the PR comment reads in the same visual language as
// the CI log.

export const MARKER = '<!-- pawl-report -->'

const cell = (v) => (v === null || v === undefined ? '—' : String(v))

const delta = (m) => {
  if (m.base === null || m.base === undefined) return 'new'
  const d = Math.round((m.current - m.base) * 100) / 100
  if (d === 0) return '±0'
  return d > 0 ? `+${d}` : `${d}`
}

// The status cell carries an icon plus the same word pawl's text table prints,
// so a metric that held, improved, or regressed reads at a glance. Unknown
// statuses fall through to their raw string rather than a blank cell.
const STATUS_CELL = {
  same: '✅ same',
  better: '🎉 better',
  worse: '❌ worse',
  new: '🆕 new',
  'within-tolerance': '✅ within tolerance',
}
const statusCell = (status) => STATUS_CELL[status] || status

// renderCommentBody turns a parsed report into the full comment body, marker
// included. Returns null when the report has no metrics array (nothing to say).
export function renderCommentBody(report) {
  if (!report || !Array.isArray(report.metrics)) return null
  const rows = report.metrics.map(
    (m) => `| ${m.id} | ${cell(m.base)} | ${cell(m.current)} | ${delta(m)} | ${statusCell(m.status)} |`,
  )
  const table = ['| metric | baseline | current | Δ | status |', '|---|---|---|---|---|', ...rows].join('\n')
  const regressed = report.metrics.filter((m) => (m.regressions || []).some((r) => !r.suppressed))
  const details = regressed.length
    ? '\n\n**Regressions**\n' +
      regressed
        .map(
          (m) =>
            `- \`${m.id}\` — ${m.title}\n` +
            m.regressions
              .filter((r) => !r.suppressed)
              .map((r) => `  - ${r.message}`)
              .join('\n'),
        )
        .join('\n')
    : ''
  const improved = report.metrics.filter((m) => m.improved).map((m) => m.id)
  const footer = improved.length
    ? `\n\n🎉 **improved:** ${improved.join(', ')} — run \`pawl record\` to lock in the gains.`
    : ''
  const verdict = report.exit_code === 0 ? '✅ no regressions' : '❌ regressions'
  return `${MARKER}\n### pawl — ${verdict}\n\n${table}${details}${footer}\n`
}
