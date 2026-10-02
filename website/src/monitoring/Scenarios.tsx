import {useState} from 'react'
import {Badge, Button, Card, ErrorState, LoadingState, Metric, Status} from '@sre-norns/components'
import {ResourceForm, type FormField} from '@sre-norns/components/forms'
import {useIdentity, type ApiValue} from '@sre-norns/components/identity'
import {Link, useNavigate, useParams} from 'react-router-dom'
import {parse, stringify} from 'yaml'
import {z} from 'zod'
import {request} from '../api/http'
import {placement, prob, run, scenario, selector, type Scenario} from '../api/models'
import {resourcePath, useResource} from '../api/queries'
import {projectPath} from '../identity/links'
import {Collection, EditResource, Heading, Labels} from './common'

export function Scenarios() {
  const {accountId = '', projectId = ''} = useParams()
  const base = projectPath(accountId, projectId)
  return <>
    <Heading title="Scenarios" description="Synthetic checks scheduled onto this project’s authorized runners."><Link className="button primary" to={`${base}/scenarios/new`}>Create scenario</Link></Heading>
    <Collection path={resourcePath('projects', projectId, 'scenarios')} scope={projectId} schema={scenario} title="Scenarios" columns={[
      {header: 'Scenario', cell: (s) => <><Link to={`${base}/scenarios/${encodeURIComponent(s.metadata.name)}`}>{s.metadata.name}</Link><small className="block muted">{s.spec.description}</small></>},
      {header: 'State', cell: (s) => <Status value={s.spec.active ? 'active' : 'disabled'} />},
      {header: 'Probe', cell: (s) => s.spec.prob?.kind || 'Not configured'},
      {header: 'Schedule', cell: (s) => s.spec.schedule || 'Manual'},
      {header: 'Recent runs', cell: (s) => <div className="tags">{s.status?.results?.slice(0, 8).map((r) => <Link key={r.metadata.uid} to={`${base}/runs/${encodeURIComponent(r.metadata.name)}`} title={r.metadata.name}><Badge tone={r.status?.result === 'success' ? 'success' : r.status?.result ? 'error' : 'neutral'}>{r.status?.result || r.status?.status || 'unknown'}</Badge></Link>)}</div>},
      {header: 'Labels', cell: (s) => <Labels value={s.metadata.labels} />},
    ]} />
  </>
}
export function ScenarioDetail() {
  const {accountId = '', projectId = '', scenarioName = ''} = useParams()
  const {session, queryClient} = useIdentity()
  const navigate = useNavigate()
  const path = resourcePath('projects', projectId, 'scenarios', scenarioName)
  const query = useResource(path, scenario, projectId, {poll: true})
  const preflight = useResource(`${path}/placement`, placement, projectId, {poll: true})
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>()
  const base = projectPath(accountId, projectId)
  async function runNow() {
    setBusy(true); setError(undefined)
    try {
      // Placement may have changed since the last poll. Refresh before enqueueing.
      const check = await request(session, `${path}/placement`, placement)
      if (!check.data.schedulable) throw new Error(check.data.detail || check.data.reason || 'No eligible runner is available.')
      const created = await request(session, `${path}/results`, run, {method: 'POST', body: {apiVersion: 'urth.sre-norns.com/v1', kind: 'results', metadata: {labels: {trigger: 'manual', triggerAgent: 'website'}}, spec: {}}})
      void queryClient.invalidateQueries({queryKey: [accountId, projectId]})
      void navigate(`${base}/runs/${encodeURIComponent(created.data.metadata.name)}`)
    } catch (error) {setError(error)} finally {setBusy(false)}
  }
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />
  if (!query.data) return <LoadingState />
  const s = query.data.data
  return <>
    <Heading title={s.metadata.name} description={s.spec.description}><div className="actions">
      <Link className="button secondary" to={`${base}/scenarios/${encodeURIComponent(scenarioName)}/edit`}>Edit scenario</Link>
      <Button disabled={busy || !preflight.data?.data.schedulable || preflight.isError} onClick={() => void runNow()}>Run now</Button>
    </div></Heading>
    {Boolean(error) && <ErrorState error={error} />}
    {preflight.isError && <ErrorState error={preflight.error} retry={() => void preflight.refetch()} />}
    <div className="grid"><Metric label="Scheduling" value={s.spec.active ? 'Enabled' : 'Disabled'} /><Metric label="Eligible runners" value={preflight.data?.data.eligibleRunners ?? 'Unknown'} /><Metric label="Ready workers" value={preflight.data?.data.readyWorkers ?? 'Unknown'} /></div>
    {preflight.data && !preflight.data.data.schedulable && <Card><h2>Placement unavailable</h2><p>{preflight.data.data.detail || preflight.data.data.reason || 'Grant an active runner with matching labels to this project.'}</p></Card>}
    <div className="grid"><Card><h2>Definition</h2><p>Probe: {s.spec.prob?.kind || 'Not configured'}</p><p>Schedule: {s.spec.schedule || 'Manual'}</p><p>Next scheduled run: {s.status?.nextScheduledRunTime ? new Date(s.status.nextScheduledRunTime).toLocaleString() : 'Not scheduled'}</p><pre tabIndex={0}>{stringify(s.spec.prob ?? {})}</pre></Card>
    <Card><h2>Runner requirements</h2><pre tabIndex={0}>{stringify(s.spec.requirements ?? {})}</pre><h3>Labels</h3><Labels value={s.metadata.labels} /></Card></div>
    <Link to={`${base}/scenarios/${encodeURIComponent(scenarioName)}/runs`}>View run history</Link>
  </>
}

function scenarioBody(values: Record<string, string>, source?: Scenario) {
  const probe = prob.parse({kind: values.kind, timeout: z.number().nonnegative().parse(Number(values.timeout || 0)) * 1_000_000_000, spec: parse(values.probe || '{}')})
  return {
    apiVersion: source?.apiVersion ?? 'urth.sre-norns.com/v1', kind: 'scenarios',
    metadata: {...source?.metadata, name: source?.metadata.name ?? values.name, labels: z.record(z.string(), z.string()).parse(parse(values.labels || '{}'))},
    spec: {...source?.spec, description: values.description, active: values.active === 'true', schedule: values.schedule, requirements: selector.parse(parse(values.requirements || '{}')), prob: probe},
  }
}
function defaults(s?: Scenario): Record<string, string> {
  return {name: s?.metadata.name ?? '', description: s?.spec.description ?? '', active: String(s?.spec.active ?? true), schedule: s?.spec.schedule ?? '', kind: s?.spec.prob?.kind ?? 'http', timeout: String((s?.spec.prob?.timeout ?? 30_000_000_000) / 1_000_000_000), probe: stringify(s?.spec.prob?.spec ?? {target: 'https://example.com/health', http: {method: 'GET', IPProtocolFallback: true}}), requirements: stringify(s?.spec.requirements ?? {}), labels: stringify(s?.metadata.labels ?? {})}
}
export function ScenarioForm() {
  const {accountId = '', projectId = '', scenarioName} = useParams()
  const {session, queryClient} = useIdentity()
  const navigate = useNavigate()
  const path = resourcePath('projects', projectId, 'scenarios', scenarioName)
  const existing = useResource(path, scenario, projectId, {enabled: Boolean(scenarioName)})
  const kinds = useResource('/v1/probs', z.object({data: z.array(z.object({kind: z.string(), contentType: z.string().optional()}))}))
  // Capture once. Background refreshes must not move the edit precondition.
  const [snapshot, setSnapshot] = useState<ApiValue<Scenario>>()
  if (existing.data && !snapshot) setSnapshot(existing.data)
  const base = projectPath(accountId, projectId)
  const finish = (name: string) => {void queryClient.invalidateQueries({queryKey: [accountId, projectId]}); void navigate(`${base}/scenarios/${encodeURIComponent(name)}`)}
  if (kinds.isError || existing.isError) return <ErrorState error={kinds.error || existing.error} />
  if (!kinds.data || (scenarioName && !snapshot)) return <LoadingState />
  const fields: FormField[] = [
    ...(!scenarioName ? [{name: 'name', label: 'Name', required: true}] : []),
    {name: 'description', label: 'Description', type: 'textarea'},
    {name: 'active', label: 'Scheduling', type: 'select', options: [{value: 'true', label: 'Enabled'}, {value: 'false', label: 'Disabled'}]},
    {name: 'schedule', label: 'Schedule', hint: 'Cron expression or interval such as @every 5m. Leave empty for manual runs.'},
    {name: 'kind', label: 'Probe kind', required: true, type: 'select', options: kinds.data.data.data.map((p) => ({value: p.kind, label: p.kind}))},
    {name: 'timeout', label: 'Timeout (seconds)', type: 'number', min: 0, step: 'any'},
    {name: 'probe', label: 'Probe specification (YAML)', type: 'textarea', hint: 'HTTP/TCP/DNS probes use target and their kind-specific options. Script probes use a script field; YAML block scalars preserve multiline scripts.'},
    {name: 'requirements', label: 'Runner requirements (YAML)', type: 'textarea', hint: 'Use matchLabels or matchExpressions.'},
    {name: 'labels', label: 'Labels (YAML)', type: 'textarea'},
  ]
  if (snapshot && scenarioName) return <><Heading title={`Edit ${scenarioName}`} /><EditResource title="Edit scenario" path={path} schema={scenario} snapshot={snapshot} fields={fields} defaults={defaults(snapshot.data)} build={scenarioBody} close={() => void navigate(`${base}/scenarios/${encodeURIComponent(scenarioName)}`)} saved={() => finish(scenarioName)} /></>
  return <><Heading title="Create scenario" description="Configure a synthetic check and the runners allowed to execute it." /><Card>
    <ResourceForm fields={fields} defaults={defaults()} build={(values) => scenarioBody(values)} submit={(body) => request(session, path, scenario, {method: 'POST', body})} onSuccess={(saved) => finish(saved.data.metadata.name)} secondaryAction={<Link to={`${base}/scenarios`}>Cancel</Link>} />
  </Card></>
}
