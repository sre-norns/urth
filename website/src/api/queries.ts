import {listQueryToParams, type ListQuery} from '@sre-norns/components'
import {useAccountId, useIdentity, useSessionEpoch} from '@sre-norns/components/identity'
import {useQuery} from '@tanstack/react-query'
import {z} from 'zod'
import {ApiProblem, request} from './http'

export function resourcePath(scope: 'accounts' | 'projects', id: string, collection: string, name?: string) {
  return `/v1/${scope}/${encodeURIComponent(id)}/${encodeURIComponent(collection)}${name === undefined ? '' : `/${encodeURIComponent(name)}`}`
}
export function listPath(path: string, query: ListQuery) {
  const params = listQueryToParams({...query, limit: query.limit ?? 20})
  // bark accepts RFC3339 seconds; datetime-local values are converted by the toolbar.
  for (const key of ['from', 'till']) {
    const time = params.get(key)
    if (time) params.set(key, time.replace(/\.\d+Z$/, 'Z'))
  }
  return `${path}?${params}`
}

/** Scope and session generation distinguish every cached product response. */
export function useResource<T>(path: string, schema: z.ZodType<T>, scope = 'account', options: {enabled?: boolean; poll?: boolean} = {}) {
  const {session, queryClient} = useIdentity()
  const account = useAccountId()
  const epoch = useSessionEpoch()
  return useQuery({
    queryKey: [account, scope, path, 'manifest', epoch],
    queryFn: ({signal}) => request(session, path, schema, undefined, signal),
    enabled: Boolean(account) && options.enabled !== false,
    retry: (count, error) => count < 1 && !(error instanceof ApiProblem && error.status < 500),
    refetchInterval: options.poll ? 15_000 : false,
    refetchIntervalInBackground: false,
  }, queryClient)
}
