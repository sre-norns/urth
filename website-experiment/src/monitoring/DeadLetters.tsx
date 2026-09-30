import {useState} from 'react'
import {Button, Card, Dialog, ErrorState, LoadingState, Status} from '@sre-norns/components'
import {useIdentity} from '@sre-norns/components/identity'
import {Link, useParams} from 'react-router-dom'
import {z} from 'zod'
import {request} from '../api/http'
import {failure, run} from '../api/models'
import {resourcePath, useResource} from '../api/queries'
import {accountPath, projectPath} from '../identity/links'
import {Collection, Heading, Labels} from './common'

function useFailureScope() {
  const {accountId = '', projectId, failureName} = useParams()
  return {
    accountId, projectId, failureName,
    scope: projectId ?? 'account',
    path: resourcePath(projectId ? 'projects' : 'accounts', projectId ?? accountId, 'dispatch-failures', failureName),
    base: projectId ? projectPath(accountId, projectId) : accountPath(accountId),
  }
}
export function DeadLetters() {
  const {path, base, scope, projectId} = useFailureScope()
  return <><Heading title="Dead letters" description={projectId ? 'Dispatch failures for this project. Retry creates a new run from the original execution snapshot.' : 'Unassignable dispatch diagnostics. Project-owned failures appear within their projects.'} />
    <Collection path={path} schema={failure} scope={scope} title="Dead letters" columns={[
      {header: 'Failure', cell: (f) => <Link to={`${base}/dead-letters/${encodeURIComponent(f.metadata.name)}`}>{f.metadata.name}</Link>},
      {header: 'Reason', cell: (f) => f.spec.reason},
      {header: 'Occurred', cell: (f) => new Date(f.spec.occurredAt).toLocaleString()},
      {header: 'State', cell: (f) => <Status value={f.status?.resolved ? 'resolved' : 'unresolved'} />},
    ]} /></>
}
export function DeadLetterDetail() {
  const {accountId, projectId, path, base, scope} = useFailureScope()
  const {session, queryClient} = useIdentity()
  const query = useResource(path, failure, scope, {poll: true})
  const [action, setAction] = useState<'retry' | 'resolve'>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>()
  async function confirm() {
    if (!action) return
    setBusy(true); setError(undefined)
    try {
      if (action === 'retry') await request(session, `${path}/retry`, z.object({failure, retry: run}), {method: 'POST', body: {resolve: true}})
      else await request(session, `${path}/resolve`, failure, {method: 'POST'})
      await queryClient.invalidateQueries({queryKey: [accountId, scope]})
      setAction(undefined)
    } catch (error) {setError(error)} finally {setBusy(false)}
  }
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />
  if (!query.data) return <LoadingState />
  const f = query.data.data
  return <>
    <Heading title={f.metadata.name} description={f.spec.reason} />
    <Card><h2>Dispatch failure</h2><Status value={f.status?.resolved ? 'resolved' : 'unresolved'} /><p>{f.spec.detail}</p>
      <p>Occurred {new Date(f.spec.occurredAt).toLocaleString()}</p><Labels value={f.metadata.labels} />
      {projectId && f.spec.resultUID && <p><Link to={`${base}/runs/${encodeURIComponent(f.spec.resultUID)}`}>Original run</Link></p>}
      {projectId && f.status?.retryResultUID && <p><Link to={`${base}/runs/${encodeURIComponent(f.status.retryResultUID)}`}>Retry run</Link></p>}
      {!f.status?.resolved && <div className="actions">
        {projectId && f.spec.resultUID && <Button onClick={() => {setError(undefined); setAction('retry')}}>Retry dispatch</Button>}
        <Button variant="secondary" onClick={() => {setError(undefined); setAction('resolve')}}>Resolve</Button>
      </div>}
    </Card>
    {action && <Dialog title={action === 'retry' ? 'Retry dispatch' : 'Resolve dead letter'} onClose={() => !busy && setAction(undefined)}>
      <p>{action === 'retry' ? 'Create a new run from the failed run’s original execution snapshot and resolve this failure?' : 'Mark this failure resolved without creating another run?'}</p>
      {Boolean(error) && <ErrorState error={error} />}
      <Button disabled={busy} onClick={() => void confirm()}>Confirm</Button>
    </Dialog>}
  </>
}
