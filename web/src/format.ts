// fmtUSD renders a USD amount with enough precision for sub-cent requests.
export function fmtUSD(v: number | null | undefined): string {
  if (v == null) return '—'
  if (v === 0) return '$0.00'
  if (v < 0.000001) return '<$0.000001'
  if (v < 0.01) return '$' + v.toFixed(6)
  if (v < 1) return '$' + v.toFixed(4)
  return '$' + v.toFixed(2)
}
