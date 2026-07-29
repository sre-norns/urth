import {screen} from '@testing-library/react'
import {http, HttpResponse} from 'msw'
import {describe, expect, it} from 'vitest'
import {App} from '../App'
import {labels} from '../labels'
import {renderApp} from '../test/render'
import {server} from '../test/server'

const current = {
  name: 'manual-current',
  uid: 'run-current',
  creationTimestamp: '2026-07-29T00:00:00Z',
  labels: {[labels.scenario.name]: 'checkout'},
  spec: {probKind: 'http', start_time: '2026-07-29T00:00:01Z', end_time: '2026-07-29T00:00:02.5Z'},
  status: {
    status: 'completed',
    result: 'success',
    numberArtifacts: 1,
    executor: {runnerId: 'runner-uid', runnerName: 'prod', workerId: 'worker-uid', workerName: 'worker-01'},
  },
}
const previous = {
  ...current,
  name: 'scheduled-previous',
  uid: 'run-previous',
  spec: {...current.spec, end_time: '2026-07-29T00:00:04Z'},
  status: {...current.status, result: 'failed', numberArtifacts: 0},
}

describe('run detail', () => {
  it('renders a newly created run when the API omits its empty artifact data', async () => {
    const pending = {
      ...current,
      name: 'manual-pending',
      uid: 'run-pending',
      spec: {probKind: 'http'},
      status: {status: 'pending', numberArtifacts: 0},
    }
    server.use(
      http.get('http://localhost/api/v1/results/manual-pending', () => HttpResponse.json(pending)),
      http.get('http://localhost/api/v1/artifacts', () => HttpResponse.json({})),
      http.get('http://localhost/api/v1/scenarios/checkout/results', () =>
        HttpResponse.json({data: [pending], total: 1}),
      ),
      http.get(
        'http://localhost/api/v1/scenarios/checkout/results/manual-pending/logs',
        () => new HttpResponse('', {headers: {'content-type': 'text/plain'}}),
      ),
    )

    renderApp(<App />, '/runs/manual-pending')

    expect(await screen.findByRole('heading', {name: /manual-pending/})).toBeInTheDocument()
    expect(await screen.findByText('No artifacts')).toBeInTheDocument()
  })

  it('links the current worker with presence and compares the previous run', async () => {
    server.use(
      http.get('http://localhost/api/v1/results/manual-current', () => HttpResponse.json(current)),
      http.get('http://localhost/api/v1/artifacts', () => HttpResponse.json({data: [], total: 0})),
      http.get('http://localhost/api/v1/scenarios/checkout/results', () => HttpResponse.json({data: [current, previous], total: 2})),
      http.get('http://localhost/api/v1/workers/worker-01', () => HttpResponse.json({
        metadata: {name: 'worker-01', uid: 'worker-uid'},
        spec: {},
        status: {presence: {api: 'online', nats: 'online', condition: 'online'}},
      })),
      http.get('http://localhost/api/v1/scenarios/checkout/results/manual-current/logs', () => new HttpResponse('probe completed', {headers: {'content-type': 'text/plain'}})),
    )
    renderApp(<App />, '/runs/manual-current')

    expect(await screen.findByRole('heading', {name: /manual-current/})).toBeInTheDocument()
    expect(await screen.findByRole('link', {name: 'worker-01'})).toHaveAttribute('href', '/workers/worker-01')
    expect(await screen.findByText('online')).toBeInTheDocument()
    expect(await screen.findByText('failed → success')).toBeInTheDocument()
    expect(await screen.findByText('probe completed')).toBeInTheDocument()
  })

  it('labels a replaced worker registration as historical', async () => {
    server.use(
      http.get('http://localhost/api/v1/results/manual-current', () => HttpResponse.json(current)),
      http.get('http://localhost/api/v1/artifacts', () => HttpResponse.json({data: [], total: 0})),
      http.get('http://localhost/api/v1/scenarios/checkout/results', () => HttpResponse.json({data: [current], total: 1})),
      http.get('http://localhost/api/v1/workers/worker-01', () => HttpResponse.json({metadata: {name: 'worker-01', uid: 'replacement'}, spec: {}, status: {presence: {condition: 'online'}}})),
      http.get('http://localhost/api/v1/scenarios/checkout/results/manual-current/logs', () => new HttpResponse('', {headers: {'content-type': 'text/plain'}})),
    )
    renderApp(<App />, '/runs/manual-current')
    expect(await screen.findByText('historical presence unknown')).toBeInTheDocument()
  })
})
