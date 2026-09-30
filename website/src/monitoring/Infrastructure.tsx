import {useState} from 'react'
import {Button, Card, Dialog, ErrorState, LastSeen, LoadingState, Metric, Status} from '@sre-norns/components'
import {ResourceForm} from '@sre-norns/components/forms'
import {MachineIdentityDetail, useIdentity, useIdentityQuery, wire, type ApiValue} from '@sre-norns/components/identity'
import {Link, useNavigate, useParams} from 'react-router-dom'
import {z} from 'zod'
import {pauseWorker, request} from '../api/http'
import {page, runner, selector, worker, type Runner} from '../api/models'
import {listPath, resourcePath, useResource} from '../api/queries'
import {accountPath} from '../identity/links'
import {runnerRegistration} from '../identity/config'
import {Collection, EditResource, Heading, Labels, Presence} from './common'

export function Runners() {
  const {accountId = ''} = useParams()
  const {session, queryClient} = useIdentity()
  const navigate = useNavigate()
  const [creating, setCreating] = useState(false)
  const path = resourcePath('accounts', accountId, 'runners')
  return <>
    <Heading title="Runners" description="Scheduling channels and their worker capacity."><Button onClick={() => setCreating(true)}>Register runner</Button></Heading>
    <Collection path={path} schema={runner} title="Runners" columns={[
      {header: 'Runner', cell: (r) => <Link to={`${accountPath(accountId)}/runners/${encodeURIComponent(r.metadata.uid)}`}>{r.metadata.name}</Link>},
      {header: 'Scheduling', cell: (r) => <Status value={r.spec.active ? 'active' : 'disabled'} />},
      {header: 'Worker limit', cell: (r) => r.spec.maxInstance || 'Unlimited'},
      {header: 'Queue', cell: (r) => r.status?.channel?.observed ? `${r.status.channel.pending ?? 0} pending` : 'Not observed'},
      {header: 'Labels', cell: (r) => <Labels value={r.metadata.labels} />},
    ]} />
    {creating && <Dialog title="Register runner" onClose={() => setCreating(false)}><ResourceForm fields={runnerRegistration.fields!}
      build={runnerRegistration.body} submit={(body) => request(session, path, runner, {method: 'POST', body})}
      onSuccess={(value) => {void queryClient.invalidateQueries({queryKey: [accountId, 'account']}); void navigate(`${accountPath(accountId)}/runners/${encodeURIComponent(value.data.metadata.uid)}`)}} /></Dialog>}
  </>
}

export function RunnerDetail() {
  const {accountId = '', runnerId = ''} = useParams()
  const {queryClient, paths} = useIdentity()
  const identity = useIdentityQuery(paths.machineIdentity(runnerId), wire.machineIdentity)
  const path = resourcePath('accounts', accountId, 'runners', identity.data?.data.name ?? '')
  const query = useResource(path, runner, 'account', {poll: true, enabled: Boolean(identity.data)})
  const workers = useResource(listPath(resourcePath('accounts', accountId, 'workers'), {labels: `urth/runner.uid=${runnerId}`, limit: 1}), page(worker), 'account', {poll: true})
  const [editing, setEditing] = useState<ApiValue<Runner>>()
  if (identity.isError) return <ErrorState error={identity.error} retry={() => void identity.refetch()} />
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />
  if (!query.data) return <LoadingState />
  const r = query.data.data
  return <>
    <MachineIdentityDetail id={r.metadata.uid} />
    <Card><h2>Scheduling</h2><p>{r.spec.description || 'No description'}</p><Status value={r.spec.active ? 'active' : 'disabled'} />
      <div className="grid"><Metric label="Registered workers" value={workers.data?.data.total ?? 'Unknown'} /><Metric label="Worker limit" value={r.spec.maxInstance || 'Unlimited'} /><Metric label="Pending messages" value={r.status?.channel?.observed ? r.status.channel.pending ?? 0 : 'Not observed'} /></div>
      <Labels value={r.metadata.labels} />
      <p>Worker requirements</p><pre tabIndex={0}>{JSON.stringify(r.spec.requirements ?? {}, null, 2)}</pre>
      <div className="actions"><Button onClick={() => setEditing(query.data)}>Edit scheduling</Button><Link to={`${accountPath(accountId)}/workers?${new URLSearchParams({labels: `urth/runner.uid=${r.metadata.uid}`})}`}>View workers</Link></div>
    </Card>
    {editing && <EditResource title="Edit runner" path={path} schema={runner} snapshot={editing}
      fields={[
        {name: 'description', label: 'Description', type: 'textarea'},
        {name: 'active', label: 'Scheduling', type: 'select', options: [{value: 'true', label: 'Enabled'}, {value: 'false', label: 'Disabled'}]},
        {name: 'maxInstance', label: 'Worker limit (0 means unlimited)', type: 'number', min: 0},
        {name: 'labels', label: 'Labels (JSON)', type: 'textarea'},
        {name: 'requirements', label: 'Worker requirements (JSON)', type: 'textarea'},
      ]}
      defaults={{description: editing.data.spec.description ?? '', active: String(editing.data.spec.active), maxInstance: String(editing.data.spec.maxInstance ?? 0), labels: JSON.stringify(editing.data.metadata.labels ?? {}, null, 2), requirements: JSON.stringify(editing.data.spec.requirements ?? {}, null, 2)}}
      build={(values, source) => ({...source, metadata: {...source.metadata, labels: z.record(z.string(), z.string()).parse(JSON.parse(values.labels || '{}'))}, spec: {...source.spec, description: values.description, active: values.active === 'true', maxInstance: z.number().int().nonnegative().parse(Number(values.maxInstance)), requirements: selector.parse(JSON.parse(values.requirements || '{}'))}})}
      close={() => setEditing(undefined)} saved={() => void queryClient.invalidateQueries({queryKey: [accountId, 'account']})} />}
  </>
}

export function Workers() {
  const {accountId = ''} = useParams()
  return <><Heading title="Workers" description="API and NATS presence are observed independently." />
    <Collection path={resourcePath('accounts', accountId, 'workers')} schema={worker} title="Workers" columns={[
      {header: 'Worker', cell: (w) => <Link to={`${accountPath(accountId)}/workers/${encodeURIComponent(w.metadata.name)}`}>{w.metadata.name}</Link>},
      {header: 'Presence', cell: (w) => <Presence value={w.status?.presence?.condition ?? 'unknown'} />},
      {header: 'Runner', cell: (w) => w.metadata.labels?.['urth/runner.uid'] ? <Link to={`${accountPath(accountId)}/runners/${encodeURIComponent(w.metadata.labels['urth/runner.uid'])}`}>{w.metadata.labels['urth/runner.name'] || 'Runner'}</Link> : 'Unknown'},
      {header: 'Last API contact', cell: (w) => <LastSeen value={w.status?.lastSeenTime} />},
      {header: 'State', cell: (w) => <Status value={w.status?.paused ? 'paused' : 'available'} />},
    ]} /></>
}

export function WorkerDetail() {
  const {accountId = '', workerName = ''} = useParams()
  const {session, queryClient} = useIdentity()
  const path = resourcePath('accounts', accountId, 'workers', workerName)
  const query = useResource(path, worker, 'account', {poll: true})
  const [error, setError] = useState<unknown>()
  const [busy, setBusy] = useState(false)
  async function pause() {
    if (!query.data) return
    setBusy(true); setError(undefined)
    try {await pauseWorker(session, `${path}/paused`, !query.data.data.status?.paused, worker); await queryClient.invalidateQueries({queryKey: [accountId, 'account']})}
    catch (error) {setError(error)} finally {setBusy(false)}
  }
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />
  if (!query.data) return <LoadingState />
  const w = query.data.data
  return <>
    <Heading title={w.metadata.name} description="Worker process"><Button disabled={busy} onClick={() => void pause()}>{w.status?.paused ? 'Resume worker' : 'Pause worker'}</Button></Heading>
    {Boolean(error) && <ErrorState error={error} />}
    <Card><h2>Presence</h2><Presence value={w.status?.presence?.condition ?? 'unknown'} />
      <h3>API server</h3><Presence value={w.status?.presence?.api ?? 'unknown'} /><LastSeen value={w.status?.lastSeenTime} /><p>Evidence: {w.status?.lastSeenVia || 'not observed'}</p>
      <h3>NATS queue</h3><Presence value={w.status?.presence?.nats ?? 'unknown'} /><LastSeen value={w.status?.natsLastSeenTime} />
      {w.status?.leftAt && <p>Departed: {new Date(w.status.leftAt).toLocaleString()}</p>}
    </Card>
    <Card><h2>Process identity</h2><p>UID: <code>{w.metadata.uid}</code></p><Labels value={w.metadata.labels} /></Card>
  </>
}
