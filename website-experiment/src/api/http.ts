import {ApiProblem, type ApiValue, type SessionClient} from '@sre-norns/components/identity'
import {z} from 'zod'

export {ApiProblem}
export type {ApiValue}

export type Write =
  | {method: 'POST'; body?: unknown}
  | {method: 'PUT'; body: unknown; etag: string}

/** Product PUTs use the ETag of the document being edited, never a fresh poll. */
export async function request<T>(
  session: SessionClient,
  path: string,
  schema: z.ZodType<T>,
  write?: Write,
  signal?: AbortSignal,
): Promise<ApiValue<T>> {
  const headers = new Headers({Accept: 'application/json'})
  if (write) {
    headers.set('Content-Type', 'application/json')
    if (write.method === 'PUT') {
      if (!write.etag || write.etag === '*') throw new Error('Load the resource before saving changes.')
      headers.set('If-Match', write.etag)
    }
  }
  const response = await session.authenticatedFetch(path, {
    method: write?.method ?? 'GET', headers, signal,
    ...(write?.body !== undefined ? {body: JSON.stringify(write.body)} : {}),
  })
  const body: unknown = response.status === 204 ? null : await response.json().catch(() => null)
  if (!response.ok) throw new ApiProblem(response.status, body, response.headers.get('Retry-After'))
  const parsed = schema.safeParse(body)
  if (!parsed.success) throw new Error('The service returned an unsupported response. Refresh and try again.')
  return {data: parsed.data, etag: response.headers.get('ETag')}
}

/** Pause is intentionally unversioned: registration continually rewrites workers. */
export async function pauseWorker<T>(session: SessionClient, path: string, paused: boolean, schema: z.ZodType<T>) {
  const response = await session.authenticatedFetch(path, {
    method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({paused}),
  })
  const body: unknown = await response.json().catch(() => null)
  if (!response.ok) throw new ApiProblem(response.status, body)
  return schema.parse(body)
}
