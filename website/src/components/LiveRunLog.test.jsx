import React from 'react'
import {screen, act} from '@testing-library/react'
import {renderWithProviders} from '../test/render.jsx'
import {describe, it, expect, beforeEach, afterEach, vi} from 'vitest'
import LiveRunLog from './LiveRunLog.jsx'

// A minimal stand-in for the browser's EventSource, so the panel's stream
// handling can be driven from a test. jsdom has no SSE implementation.
class FakeEventSource {
  static instances = []

  constructor(url) {
    this.url = url
    this.readyState = 0
    this.listeners = {}
    this.onmessage = null
    this.onerror = null
    this.closed = false
    FakeEventSource.instances.push(this)
  }

  addEventListener(name, handler) {
    this.listeners[name] = handler
  }

  close() {
    this.closed = true
    this.readyState = 2
  }

  emitMessage(data) {
    act(() => this.onmessage?.({data}))
  }

  emitEnd() {
    act(() => this.listeners.end?.({}))
  }
}
FakeEventSource.CLOSED = 2

describe('LiveRunLog', () => {
  beforeEach(() => {
    FakeEventSource.instances = []
    vi.stubGlobal('EventSource', FakeEventSource)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('subscribes to the run log endpoint for the given scenario and run', () => {
    renderWithProviders(<LiveRunLog runId="run-1" scenarioName="my-scenario" isRunning />)

    expect(FakeEventSource.instances).toHaveLength(1)
    expect(FakeEventSource.instances[0].url).toBe('/api/v1/scenarios/my-scenario/results/run-1/logs')
  })

  it('renders lines as they arrive', () => {
    renderWithProviders(<LiveRunLog runId="run-1" scenarioName="my-scenario" isRunning />)

    const source = FakeEventSource.instances[0]
    source.emitMessage('first line')
    source.emitMessage('second line')

    const surface = screen.getByTestId('run-log-surface')
    expect(surface.textContent).toContain('first line')
    expect(surface.textContent).toContain('second line')
  })

  // The run may have finished before the page was opened, in which case the
  // server sends the stored artifact and then an end event. Both paths go
  // through this component, so it must not keep reporting "streaming".
  it('stops reporting streaming once the stream ends', () => {
    renderWithProviders(<LiveRunLog runId="run-1" scenarioName="my-scenario" isRunning />)

    const source = FakeEventSource.instances[0]
    source.emitMessage('only line')
    source.emitEnd()

    expect(screen.queryByText('streaming…')).toBeNull()
    expect(screen.getByText('1 lines')).toBeTruthy()
    expect(source.closed).toBe(true)
  })

  // Names go into a URL path, so a run or scenario containing a slash or a
  // space must not be able to change which endpoint is called.
  it('escapes names in the endpoint URL', () => {
    renderWithProviders(<LiveRunLog runId="run/../../etc" scenarioName="a scenario" isRunning />)

    expect(FakeEventSource.instances[0].url).toBe('/api/v1/scenarios/a%20scenario/results/run%2F..%2F..%2Fetc/logs')
  })

  it('closes the stream when unmounted', () => {
    const {unmount} = renderWithProviders(<LiveRunLog runId="run-1" scenarioName="my-scenario" isRunning />)

    const source = FakeEventSource.instances[0]
    unmount()

    expect(source.closed).toBe(true)
  })

  it('does not subscribe until the scenario is known', () => {
    renderWithProviders(<LiveRunLog runId="run-1" isRunning />)

    expect(FakeEventSource.instances).toHaveLength(0)
  })
})
