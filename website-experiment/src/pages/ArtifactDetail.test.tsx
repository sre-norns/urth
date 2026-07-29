import {screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {http, HttpResponse} from 'msw'
import {describe, expect, it, vi} from 'vitest'
import {App} from '../App'
import {labels} from '../labels'
import {renderApp} from '../test/render'
import {server} from '../test/server'

describe('artifact content handling', () => {
  it('does not fetch secret-bearing content before the operator reveals it', async () => {
    const contentRequest = vi.fn()
    server.use(
      http.get('http://localhost/api/v1/artifacts/capture.har', () => HttpResponse.json({
        kind: 'artifacts',
        metadata: {
          name: 'capture.har',
          uid: 'artifact-1',
          labels: {
            [labels.artifact.kind]: 'har',
            [labels.artifact.mime]: 'application/json',
            [labels.artifact.dataClass]: 'secret-bearing',
            [labels.artifact.resultName]: 'run-1',
          },
        },
        spec: {rel: 'har', mimeType: 'application/json', dataClass: 'secret-bearing'},
      })),
      http.get('http://localhost/api/v1/artifacts/capture.har/content', () => {
        contentRequest()
        return new HttpResponse('{"token":"sensitive"}', {headers: {'content-type': 'application/json'}})
      }),
    )
    renderApp(<App />, '/artifacts/capture.har')

    expect(await screen.findByText('This artifact may contain secrets')).toBeInTheDocument()
    expect(contentRequest).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', {name: 'Reveal sensitive content'}))
    expect(await screen.findByText(/"token":"sensitive"/)).toBeInTheDocument()
    expect(contentRequest).toHaveBeenCalledOnce()
  })
})
