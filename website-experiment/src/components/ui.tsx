import type {ButtonHTMLAttributes, HTMLAttributes, ReactNode} from 'react'
import {AlertTriangle, LoaderCircle, RotateCcw} from 'lucide-react'
import {Link} from 'react-router-dom'
import type {Labels, PresenceCondition, Run} from '../types'
import {presenceLabel, statusTone} from '../utils'

export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow?: ReactNode
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
}) {
  return (
    <header className="page-header">
      <div>
        {eyebrow && <div className="eyebrow">{eyebrow}</div>}
        <h1>{title}</h1>
        {description && <p>{description}</p>}
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </header>
  )
}

export function Button({
  tone = 'primary',
  className = '',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  tone?: 'primary' | 'quiet' | 'danger'
}) {
  return <button className={`button button-${tone} ${className}`} {...props} />
}

export function LinkButton({
  to,
  children,
  tone = 'primary',
}: {
  to: string
  children: ReactNode
  tone?: 'primary' | 'quiet'
}) {
  return (
    <Link className={`button button-${tone}`} to={to}>
      {children}
    </Link>
  )
}

export function Card({
  title,
  meta,
  children,
  className = '',
  ...props
}: Omit<HTMLAttributes<HTMLElement>, 'title'> & {
  title?: ReactNode
  meta?: ReactNode
  children: ReactNode
}) {
  return (
    <section className={`card ${className}`} {...props}>
      {(title || meta) && (
        <div className="card-header">
          {title && <h2>{title}</h2>}
          {meta && <div className="card-meta">{meta}</div>}
        </div>
      )}
      {children}
    </section>
  )
}

export function Status({
  value,
  run,
  subtle = false,
  title,
}: {
  value?: string
  run?: Run
  subtle?: boolean
  title?: string
}) {
  const label = value || (run?.status?.result ?? run?.status?.status) || 'unknown'
  const tone = statusTone(run ?? label)
  return (
    <span className={`status status-${tone} ${subtle ? 'status-subtle' : ''}`} title={title}>
      <span className="status-dot" aria-hidden="true" />
      {label}
    </span>
  )
}

export function Presence({condition}: {condition: PresenceCondition}) {
  return <Status value={presenceLabel(condition)} title={`Current presence: ${presenceLabel(condition)}`} />
}

export function Stat({
  label,
  value,
  detail,
  tone,
}: {
  label: string
  value: ReactNode
  detail?: ReactNode
  tone?: string
}) {
  return (
    <div className={`stat ${tone ? `stat-${tone}` : ''}`}>
      <span className="stat-label">{label}</span>
      <strong>{value}</strong>
      {detail && <span className="stat-detail">{detail}</span>}
    </div>
  )
}

export function LabelChips({
  labels,
  onClick,
  includeSystem = false,
}: {
  labels?: Labels
  onClick?: (key: string, value: string) => void
  includeSystem?: boolean
}) {
  const entries = Object.entries(labels ?? {}).filter(([key]) => includeSystem || !key.startsWith('urth/'))
  if (!entries.length) return <span className="muted">No labels</span>
  return (
    <div className="chips">
      {entries.map(([key, value]) =>
        onClick ? (
          <button className="chip" type="button" key={key} onClick={() => onClick(key, value)}>
            <span>{key}</span>=<strong>{value}</strong>
          </button>
        ) : (
          <span className="chip" key={key}>
            <span>{key}</span>=<strong>{value}</strong>
          </span>
        ),
      )}
    </div>
  )
}

export function Loading({label = 'Loading data'}: {label?: string}) {
  return (
    <div className="state-panel" role="status">
      <LoaderCircle className="spin" size={22} aria-hidden="true" />
      <span>{label}…</span>
    </div>
  )
}

export function ErrorState({
  error,
  title = 'Could not load this view',
  retry,
}: {
  error: unknown
  title?: string
  retry?: () => void
}) {
  const message = error instanceof Error ? error.message : String(error)
  return (
    <div className="state-panel state-error" role="alert">
      <AlertTriangle size={22} aria-hidden="true" />
      <div>
        <strong>{title}</strong>
        <span>{message}</span>
      </div>
      {retry && (
        <Button tone="quiet" onClick={retry}>
          <RotateCcw size={15} /> Retry
        </Button>
      )}
    </div>
  )
}

export function Empty({
  title = 'Nothing here yet',
  description,
  action,
}: {
  title?: string
  description?: string
  action?: ReactNode
}) {
  return (
    <div className="empty">
      <div className="empty-radar" aria-hidden="true">
        <i />
      </div>
      <strong>{title}</strong>
      {description && <p>{description}</p>}
      {action}
    </div>
  )
}

export function KeyValue({
  items,
}: {
  items: Array<{label: ReactNode; value: ReactNode; mono?: boolean}>
}) {
  return (
    <dl className="key-values">
      {items.map((item, index) => (
        <div key={index}>
          <dt>{item.label}</dt>
          <dd className={item.mono ? 'mono' : ''}>{item.value ?? '—'}</dd>
        </div>
      ))}
    </dl>
  )
}

export function Confirm({
  open,
  title,
  description,
  confirmLabel,
  busy,
  onCancel,
  onConfirm,
}: {
  open: boolean
  title: string
  description: string
  confirmLabel: string
  busy?: boolean
  onCancel: () => void
  onConfirm: () => void
}) {
  if (!open) return null
  return (
    <div className="dialog-backdrop" role="presentation" onMouseDown={onCancel}>
      <div className="dialog" role="dialog" aria-modal="true" aria-labelledby="confirm-title" onMouseDown={(event) => event.stopPropagation()}>
        <h2 id="confirm-title">{title}</h2>
        <p>{description}</p>
        <div className="dialog-actions">
          <Button tone="quiet" onClick={onCancel} disabled={busy}>Cancel</Button>
          <Button tone="danger" onClick={onConfirm} disabled={busy}>{confirmLabel}</Button>
        </div>
      </div>
    </div>
  )
}
