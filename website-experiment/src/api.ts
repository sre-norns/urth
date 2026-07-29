import type {
  Artifact,
  ListResponse,
  Placement,
  ProbKind,
  Run,
  Runner,
  Scenario,
  SearchState,
  Worker,
} from './types'

export class ApiError extends Error {
  status: number
  detail?: unknown

  constructor(message: string, status: number, detail?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.detail = detail
  }
}

function requestInit(method: string, body?: unknown): RequestInit {
  const token =
    typeof window !== 'undefined' && typeof window.localStorage?.getItem === 'function'
      ? window.localStorage.getItem('token')
      : null
  return {
    method,
    mode: 'same-origin',
    cache: 'no-cache',
    credentials: 'omit',
    redirect: 'error',
    referrerPolicy: 'no-referrer',
    headers: {
      ...(token ? {Authorization: `Bearer ${token}`} : {}),
      ...(body === undefined ? {} : {'Content-Type': 'application/json'}),
    },
    ...(body === undefined ? {} : {body: JSON.stringify(body)}),
  }
}

async function request<T>(url: string, method = 'GET', body?: unknown): Promise<T> {
  const response = await fetch(url, requestInit(method, body))
  const isJson = response.headers.get('content-type')?.includes('application/json')
  const data = isJson ? await response.json() : undefined
  if (!response.ok) {
    throw new ApiError(
      String(data?.Message || data?.message || `Server returned ${response.status}`),
      response.status,
      data,
    )
  }
  if (response.status !== 204 && data === undefined) {
    throw new ApiError('Unexpected response format', response.status)
  }
  return data as T
}

async function requestList<T>(url: string): Promise<ListResponse<T>> {
  const response = await request<Partial<ListResponse<T>>>(url)
  return {
    data: response.data ?? [],
    total: response.total ?? 0,
  }
}

function pathPart(value: string): string {
  return encodeURIComponent(value)
}

export function searchParams(search: SearchState = {}): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(search)) {
    if (value !== undefined && value !== '') params.set(key, String(value))
  }
  const query = params.toString()
  return query ? `?${query}` : ''
}

export const api = {
  scenarios: {
    list: (search?: SearchState) =>
      requestList<Scenario>(`/api/v1/scenarios${searchParams(search)}`),
    get: (name: string) => request<Scenario>(`/api/v1/scenarios/${pathPart(name)}`),
    create: (body: Pick<Scenario, 'metadata' | 'spec'>) =>
      request<Scenario>('/api/v1/scenarios', 'POST', body),
    update: (name: string, body: Scenario) =>
      request<Scenario>(`/api/v1/scenarios/${pathPart(name)}`, 'PUT', body),
    placement: (name: string) =>
      request<Placement>(`/api/v1/scenarios/${pathPart(name)}/placement`),
    runs: (name: string, search?: SearchState) =>
      requestList<Run>(
        `/api/v1/scenarios/${pathPart(name)}/results${searchParams(search)}`,
      ),
    runNow: (name: string) =>
      request<Run>(`/api/v1/scenarios/${pathPart(name)}/results`, 'POST', {
        metadata: {
          name: 'manual-',
          labels: {trigger: 'manual', triggerAgent: 'website-experiment'},
        },
      }),
  },
  runs: {
    list: (search?: SearchState) =>
      requestList<Run>(`/api/v1/results${searchParams(search)}`),
    get: (name: string) => request<Run>(`/api/v1/results/${pathPart(name)}`),
    logs: async (scenario: string, run: string, signal?: AbortSignal) => {
      const response = await fetch(
        `/api/v1/scenarios/${pathPart(scenario)}/results/${pathPart(run)}/logs`,
        {...requestInit('GET'), signal},
      )
      if (!response.ok) throw new ApiError(`Log request returned ${response.status}`, response.status)
      return response.text()
    },
  },
  runners: {
    list: (search?: SearchState) =>
      requestList<Runner>(`/api/v1/runners${searchParams(search)}`),
    get: (name: string) => request<Runner>(`/api/v1/runners/${pathPart(name)}`),
    update: (name: string, body: Runner) =>
      request<Runner>(`/api/v1/runners/${pathPart(name)}`, 'PUT', body),
  },
  workers: {
    list: (search?: SearchState) =>
      requestList<Worker>(`/api/v1/workers${searchParams(search)}`),
    get: (name: string) => request<Worker>(`/api/v1/workers/${pathPart(name)}`),
    pause: (name: string, paused: boolean) =>
      request<Worker>(`/api/v1/workers/${pathPart(name)}/paused`, 'PUT', {paused}),
  },
  artifacts: {
    list: (search?: SearchState) =>
      requestList<Artifact>(`/api/v1/artifacts${searchParams(search)}`),
    get: (name: string) => request<Artifact>(`/api/v1/artifacts/${pathPart(name)}`),
    content: async (name: string) => {
      const response = await fetch(`/api/v1/artifacts/${pathPart(name)}/content`, requestInit('GET'))
      if (!response.ok) {
        throw new ApiError(`Artifact request returned ${response.status}`, response.status)
      }
      return {blob: await response.blob(), mime: response.headers.get('content-type') || ''}
    },
  },
  probs: () => request<{data: ProbKind[]}>('/api/v1/probs'),
}
