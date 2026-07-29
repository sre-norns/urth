import {screen} from '@testing-library/react'
import {http, HttpResponse} from 'msw'
import {describe, expect, it, vi} from 'vitest'
import {App} from '../App'
import {renderApp} from '../test/render'
import {server} from '../test/server'

describe('scenario pagination', () => {
  it('requests server page zero for the initial human-facing page', async () => {
    const requestedPage = vi.fn()
    server.use(
      http.get('http://localhost/api/v1/scenarios', ({request}) => {
        const page = new URL(request.url).searchParams.get('page')
        requestedPage(page)
        return HttpResponse.json(
          page === '0'
            ? {
                data: [
                  {
                    kind: 'scenarios',
                    metadata: {name: 'checkout-health', uid: 'scenario-1'},
                    spec: {active: true, prob: {kind: 'http'}},
                    status: {results: []},
                  },
                ],
                total: 1,
              }
            : {data: [], total: 1},
        )
      }),
    )

    renderApp(<App />, '/scenarios')

    expect(await screen.findByRole('link', {name: 'checkout-health'})).toBeInTheDocument()
    expect(requestedPage).toHaveBeenCalledWith('0')
  })
})
