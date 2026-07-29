import {Search, SlidersHorizontal, X} from 'lucide-react'

export function SearchToolbar({
  name,
  labels,
  onName,
  onLabels,
  children,
}: {
  name: string
  labels: string
  onName: (value: string) => void
  onLabels: (value: string) => void
  children?: React.ReactNode
}) {
  const active = Boolean(name || labels)
  return (
    <div className="search-toolbar">
      <label className="search-control">
        <Search size={16} />
        <span className="sr-only">Filter by name</span>
        <input value={name} onChange={(event) => onName(event.target.value)} placeholder="Filter by name…" />
      </label>
      <label className="search-control label-selector">
        <SlidersHorizontal size={16} />
        <span className="sr-only">Label selector</span>
        <input value={labels} onChange={(event) => onLabels(event.target.value)} placeholder="Labels: env = prod, team = checkout" />
      </label>
      {children}
      {active && (
        <button className="clear-filter" onClick={() => {onName(''); onLabels('')}}>
          <X size={14} /> Clear
        </button>
      )}
    </div>
  )
}
