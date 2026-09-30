import {useState, type ReactNode} from 'react'
import {Badge, Button, Card, CursorPagination, Dialog, ErrorState, RefreshStatus, ResourceTable, SearchToolbar, useListQuery, type Column} from '@sre-norns/components'
import {ResourceForm, type FormField} from '@sre-norns/components/forms'
import {isAccountAdmin, useIdentity, usePrincipal, type ApiValue} from '@sre-norns/components/identity'
import {Outlet, useLocation, useParams} from 'react-router-dom'
import {z} from 'zod'
import {ApiProblem, request} from '../api/http'
import {page} from '../api/models'
import {listPath, useResource} from '../api/queries'

export function AccountOnly() {
  const location = useLocation()
  return isAccountAdmin(usePrincipal()) ? <div className="monitoring"><Outlet key={location.pathname} /></div> : <ErrorState error={new ApiProblem(403, {detail: 'Account administration is required for infrastructure.'})} />
}
export function ProjectMemberOnly() {
  const {projectId = ''} = useParams()
  return usePrincipal().projectIds.includes(projectId) ? <div className="monitoring"><Outlet /></div> : <ErrorState error={new ApiProblem(403, {detail: 'Join this project to view its monitoring resources.'})} />
}
export function Presence({value}: {value: string}) {
  return <Badge tone={value === 'online' ? 'success' : value === 'offline' ? 'error' : value === 'unknown' ? 'neutral' : 'attention'}>{value.replaceAll('-', ' ')}</Badge>
}
export function Heading({title, description, children}: {title: string; description?: string; children?: ReactNode}) {
  return <header className="page-heading"><div><h1>{title}</h1>{description && <p>{description}</p>}</div>{children}</header>
}
export function Labels({value}: {value?: Record<string, string>}) {
  return <div className="tags">{Object.entries(value ?? {}).map(([key, value]) => <Badge key={key}>{key}={value}</Badge>)}</div>
}
export function Collection<T extends {metadata: {uid: string}}>({path, schema, title, columns, scope = 'account', children}: {
  path: string; schema: z.ZodType<T>; title: string; columns: Column<T>[]; scope?: string; children?: ReactNode;
}) {
  const list = useListQuery()
  const query = useResource(listPath(path, list.query), page(schema), scope, {poll: true})
  const data = query.data?.data
  return <>
    {children}
    <SearchToolbar value={list.query} onSearch={list.filter} timeRange />
    <RefreshStatus at={query.dataUpdatedAt} refreshing={query.isFetching} onRefresh={() => void query.refetch()} />
    <ResourceTable caption={title} columns={columns} rows={data?.items} rowKey={(row) => row.metadata.uid}
      loading={query.isPending} error={query.error} onRetry={() => void query.refetch()} />
    {data && <CursorPagination shown={data.items.length} total={data.total} next={data.next} hasPrevious={list.hasPrevious} atFirst={list.atFirst} onNext={list.next} onPrevious={list.previous} onFirst={list.first} />}
  </>
}

/** The snapshot is captured on opening; polling must not replace its If-Match. */
export function EditResource<T>({title, path, schema, snapshot, fields, defaults, build, close, saved}: {
  title: string; path: string; schema: z.ZodType<T>; snapshot: ApiValue<T>; fields: FormField[];
  defaults: Record<string, string>; build: (values: Record<string, string>, original: T) => unknown;
  close: () => void; saved: () => void;
}) {
  const {session} = useIdentity()
  const [current, setCurrent] = useState(snapshot)
  const [conflict, setConflict] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>()
  const [review, setReview] = useState<ApiValue<T>>()
  async function reload() {
    setLoading(true)
    try { setReview(await request(session, path, schema)); setError(undefined) }
    catch (error) { setError(error) }
    finally { setLoading(false) }
  }
  return <Dialog title={title} onClose={close}>
    <ResourceForm fields={fields} defaults={defaults} build={(values) => build(values, current.data)}
      submit={(body) => {
        if (conflict) return Promise.reject(new Error('Review the latest resource before applying your draft.'))
        return request(session, path, schema, {method: 'PUT', body, etag: current.etag ?? ''})
      }}
      onError={(error) => {if (error instanceof ApiProblem && error.conflict) setConflict(true)}}
      onSuccess={() => {saved(); close()}}>
      {conflict && <Card><h2>Resource changed</h2><p>Your draft is preserved. Load the latest resource and review it before applying your draft.</p>
        <Button variant="secondary" disabled={loading} onClick={() => void reload()}>Load latest resource</Button>
        {review && <><pre tabIndex={0}>{JSON.stringify(review.data, null, 2)}</pre><Button onClick={() => {setCurrent(review); setReview(undefined); setConflict(false)}}>Apply draft to this version</Button></>}
      </Card>}
      {Boolean(error) && <ErrorState error={error} />}
    </ResourceForm>
  </Dialog>
}
