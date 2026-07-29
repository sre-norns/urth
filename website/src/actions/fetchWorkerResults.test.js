import {afterEach, describe, expect, it, vi} from 'vitest'
import {LabelWorker} from '../utils/labels.js'
import fetchWorkerResults, {WORKER_RUN_LIMIT} from './fetchWorkerResults.js'

const {apiGet} = vi.hoisted(() => ({apiGet: vi.fn()}))
vi.mock('../utils/api.js', () => ({apiGet}))

afterEach(() => {
  apiGet.mockReset()
})

// The action owns the bounded query rather than relying on callers to remember
// the selector and page size.
describe('fetchWorkerResults', () => {
  it('queries the latest ten runs by worker name', async () => {
    apiGet.mockResolvedValue({data: [], total: 17})
    const dispatch = vi.fn()

    await fetchWorkerResults('agent.build-7')(dispatch)

    const url = new URL(apiGet.mock.calls[0][0], 'http://example.test')
    expect(url.pathname).toBe('/api/v1/results')
    expect(url.searchParams.get('labels')).toBe(`${LabelWorker.Name} = agent.build-7`)
    expect(url.searchParams.get('pageSize')).toBe(String(WORKER_RUN_LIMIT))
    expect(dispatch).toHaveBeenLastCalledWith({
      type: 'WORKER_RESULTS_FETCHED',
      key: 'agent.build-7',
      response: {data: [], total: 17},
    })
  })
})
