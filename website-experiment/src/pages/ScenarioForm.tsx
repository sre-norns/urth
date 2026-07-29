import {useEffect, useMemo, useState} from 'react'
import {useMutation, useQuery, useQueryClient} from '@tanstack/react-query'
import {ArrowLeft, Braces, Save} from 'lucide-react'
import {parse, stringify} from 'yaml'
import {Link, useNavigate, useParams} from 'react-router-dom'
import {api} from '../api'
import {Button, Card, ErrorState, Loading, PageHeader} from '../components/ui'
import type {Labels, Prob, Scenario, ScenarioSpec} from '../types'
import {userLabels} from '../labels'

const knownFields: Record<string, Array<{path: string; label: string; placeholder: string}>> = {
  http: [{path: 'target', label: 'Target URL', placeholder: 'https://example.com/health'}, {path: 'http.method', label: 'Method', placeholder: 'GET'}],
  tcp: [{path: 'target', label: 'Target host:port', placeholder: 'example.com:443'}],
  icmp: [{path: 'target', label: 'Target host', placeholder: 'example.com'}],
  grpc: [{path: 'target', label: 'Target host:port', placeholder: 'example.com:443'}],
  dns: [{path: 'target', label: 'DNS server', placeholder: '8.8.8.8'}, {path: 'dns.query_name', label: 'Query name', placeholder: 'example.com'}],
}
const scriptTypes = new Set(['text/javascript', 'text/x-python', 'application/http', 'application/json'])

function template(kind: string): Record<string, unknown> {
  if (kind === 'http') return {target: '', http: {method: 'GET', IPProtocolFallback: true}}
  if (['tcp', 'icmp', 'grpc'].includes(kind)) return {target: '', [kind]: {IPProtocolFallback: true}}
  if (kind === 'dns') return {target: '', dns: {query_name: '', preferred_ip_protocol: 'ip4', IPProtocolFallback: true}}
  if (kind === 'rest') return {script: 'GET https://example.com/health\n'}
  if (kind === 'puppeteer') return {script: "// await page.goto('https://example.com')\n"}
  if (kind === 'pypuppeteer') return {script: "# await page.goto('https://example.com')\n"}
  return {}
}

function getAt(value: Record<string, unknown>, path: string): string {
  return String(path.split('.').reduce<unknown>((current, key) => (current as Record<string, unknown>)?.[key], value) ?? '')
}

function setAt(value: Record<string, unknown>, path: string, nextValue: string) {
  const next = structuredClone(value)
  const keys = path.split('.')
  let current = next
  keys.slice(0, -1).forEach((key) => {
    current[key] = typeof current[key] === 'object' && current[key] ? current[key] : {}
    current = current[key] as Record<string, unknown>
  })
  if (nextValue) current[keys.at(-1)!] = nextValue
  else delete current[keys.at(-1)!]
  return next
}

function parseLabels(text: string): Labels {
  return Object.fromEntries(text.split('\n').map((line) => line.trim()).filter(Boolean).map((line) => {
    const index = line.indexOf('=')
    if (index < 1) throw new Error(`Invalid label "${line}". Use key=value.`)
    return [line.slice(0, index).trim(), line.slice(index + 1).trim()]
  }))
}

export function ScenarioFormPage() {
  const {scenarioName} = useParams()
  const isNew = !scenarioName
  const name = scenarioName ? decodeURIComponent(scenarioName) : ''
  const navigate = useNavigate()
  const client = useQueryClient()
  const existing = useQuery({queryKey: ['scenario', name], queryFn: () => api.scenarios.get(name), enabled: !isNew})
  const probKinds = useQuery({queryKey: ['prob-kinds'], queryFn: api.probs})
  const [form, setForm] = useState({name: '', description: '', active: true, schedule: '', labels: '', requirements: '{}', prob: {kind: '', spec: {}} as Prob})
  const [rawSpec, setRawSpec] = useState('{}\n')
  const [mode, setMode] = useState<'form' | 'yaml'>('form')
  const [error, setError] = useState<unknown>()

  useEffect(() => {
    if (!existing.data) return
    setForm({
      name: existing.data.metadata.name,
      description: existing.data.spec.description || '',
      active: existing.data.spec.active,
      schedule: existing.data.spec.schedule || '',
      labels: Object.entries(userLabels(existing.data.metadata.labels)).map(([key, value]) => `${key}=${value}`).join('\n'),
      requirements: stringify(existing.data.spec.requirements || {}),
      prob: existing.data.spec.prob || {kind: '', spec: {}},
    })
    setRawSpec(stringify(existing.data.spec.prob?.spec || {}))
  }, [existing.data])

  const kindInfo = probKinds.data?.data.find((entry) => entry.kind === form.prob.kind)
  const scripted = scriptTypes.has(kindInfo?.contentType || '')
  const fields = knownFields[form.prob.kind]
  const mutation = useMutation({
    mutationFn: async () => {
      const editableLabels = parseLabels(form.labels)
      const requirements = parse(form.requirements || '{}')
      const spec: ScenarioSpec = {description: form.description, active: form.active, schedule: form.schedule, requirements, prob: form.prob}
      if (isNew) return api.scenarios.create({metadata: {name: form.name, labels: editableLabels}, spec})
      const source = existing.data!
      return api.scenarios.update(name, {...source, metadata: {...source.metadata, labels: editableLabels}, spec} as Scenario)
    },
    onSuccess: (saved) => {
      client.invalidateQueries({queryKey: ['scenarios']})
      client.setQueryData(['scenario', saved.metadata.name], saved)
      navigate(`/scenarios/${encodeURIComponent(saved.metadata.name)}`)
    },
    onError: setError,
  })
  const valid = form.name.trim().length > 0 && form.name.length <= 32 && Boolean(form.prob.kind)
  const availableKinds = useMemo(() => probKinds.data?.data ?? [], [probKinds.data])

  if (!isNew && existing.isPending) return <div className="page"><Loading label="Loading scenario" /></div>
  if (!isNew && existing.isError) return <div className="page"><ErrorState error={existing.error} /></div>

  function changeKind(kind: string) {
    const spec = template(kind)
    setForm((current) => ({...current, prob: {kind, spec}}))
    setRawSpec(stringify(spec))
    setMode(knownFields[kind] ? 'form' : 'yaml')
  }

  function changeRaw(text: string) {
    setRawSpec(text)
    try {
      const spec = parse(text) || {}
      setForm((current) => ({...current, prob: {...current.prob, spec}}))
      setError(undefined)
    } catch (nextError) {
      setError(nextError)
    }
  }

  return (
    <div className="page page-narrow">
      <PageHeader eyebrow={<Link to={isNew ? '/scenarios' : `/scenarios/${encodeURIComponent(name)}`}><ArrowLeft size={13} /> {isNew ? 'All scenarios' : name}</Link>} title={isNew ? 'New scenario' : 'Edit scenario'} description="Define the target, execution channel, and schedule." />
      {Boolean(error) && <ErrorState title="Could not save scenario" error={error} />}
      <form className="form-stack" onSubmit={(event) => {event.preventDefault(); if (valid) mutation.mutate()}}>
        <Card title="Identity">
          <div className="form-grid">
            <label><span>Name <b>*</b></span><input value={form.name} disabled={!isNew} maxLength={32} onChange={(event) => setForm({...form, name: event.target.value})} placeholder="checkout-health" /></label>
            <label className="span-2"><span>Description</span><textarea rows={3} value={form.description} maxLength={128} onChange={(event) => setForm({...form, description: event.target.value})} placeholder="Checks the public checkout endpoint" /></label>
            <label className="switch-row span-2"><span><strong>Active</strong><small>Allow scheduled and manual executions.</small></span><input type="checkbox" checked={form.active} onChange={(event) => setForm({...form, active: event.target.checked})} /></label>
          </div>
        </Card>
        <Card title="Schedule and placement">
          <div className="form-grid">
            <label><span>Schedule</span><input className="mono" value={form.schedule} onChange={(event) => setForm({...form, schedule: event.target.value})} placeholder="@5minutes or cron expression" /></label>
            <label><span>User labels <small>one key=value per line</small></span><textarea className="mono" rows={5} value={form.labels} onChange={(event) => setForm({...form, labels: event.target.value})} placeholder={'team=checkout\nenv=production'} /></label>
            <label><span>Runner requirements <small>YAML selector</small></span><textarea className="mono" rows={5} value={form.requirements} onChange={(event) => setForm({...form, requirements: event.target.value})} /></label>
          </div>
        </Card>
        <Card title="Probe" meta={<span className="mono">server registry</span>}>
          <div className="form-grid">
            <label><span>Probe type <b>*</b></span><select value={form.prob.kind} onChange={(event) => changeKind(event.target.value)}><option value="">Select a probe…</option>{availableKinds.map((kind) => <option key={kind.kind} value={kind.kind}>{kind.kind}</option>)}</select></label>
            {form.prob.kind && !scripted && fields && <div className="editor-tabs span-2"><button type="button" className={mode === 'form' ? 'active' : ''} onClick={() => setMode('form')}>Form</button><button type="button" className={mode === 'yaml' ? 'active' : ''} onClick={() => setMode('yaml')}><Braces size={14} /> YAML</button></div>}
            {form.prob.kind && scripted && <label className="span-2"><span>Script</span><textarea className="code-editor" rows={14} value={getAt(form.prob.spec || {}, 'script')} onChange={(event) => setForm({...form, prob: {...form.prob, spec: setAt(form.prob.spec || {}, 'script', event.target.value)}})} /></label>}
            {form.prob.kind && !scripted && fields && mode === 'form' && fields.map((field) => <label key={field.path}><span>{field.label}</span><input value={getAt(form.prob.spec || {}, field.path)} onChange={(event) => setForm({...form, prob: {...form.prob, spec: setAt(form.prob.spec || {}, field.path, event.target.value)}})} placeholder={field.placeholder} /></label>)}
            {form.prob.kind && !scripted && (!fields || mode === 'yaml') && <label className="span-2"><span>Probe spec <small>YAML</small></span><textarea className="code-editor" rows={14} value={rawSpec} onChange={(event) => changeRaw(event.target.value)} /></label>}
          </div>
        </Card>
        <div className="form-actions">
          <Link className="button button-quiet" to={isNew ? '/scenarios' : `/scenarios/${encodeURIComponent(name)}`}>Cancel</Link>
          <Button type="submit" disabled={!valid || mutation.isPending}><Save size={16} /> {mutation.isPending ? 'Saving…' : 'Save scenario'}</Button>
        </div>
      </form>
    </div>
  )
}
