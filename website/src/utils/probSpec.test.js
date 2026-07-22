import {describe, it, expect} from 'vitest'
import {fieldsFor, getAt, isScriptKind, setAt, templateFor} from './probSpec.js'

describe('isScriptKind', () => {
  // Decided from the content type the server reports, not from a list of names,
  // so a scripted kind added server-side is handled without a UI change.
  it.each([
    [{kind: 'puppeteer', contentType: 'text/javascript'}, true],
    [{kind: 'pypuppeteer', contentType: 'text/x-python'}, true],
    [{kind: 'rest', contentType: 'application/http'}, true],
    [{kind: 'har', contentType: 'application/json'}, true],
    [{kind: 'http', contentType: 'application/yaml'}, false],
    [{kind: 'tcp', contentType: 'text/plain'}, false],
  ])('%o -> %s', (info, expected) => {
    expect(isScriptKind(info)).toBe(expected)
  })

  it('treats an unknown kind as configured rather than scripted', () => {
    expect(isScriptKind(undefined)).toBe(false)
    expect(isScriptKind({kind: 'something-new'})).toBe(false)
  })
})

describe('fieldsFor', () => {
  it('describes the configured kinds', () => {
    expect(fieldsFor('http').map((f) => f.path)).toEqual(['target', 'http.method'])
    expect(fieldsFor('dns').map((f) => f.path)).toEqual(['target', 'dns.query_name'])
  })

  // A kind the server reports but the UI does not describe must still be
  // editable, through the YAML fallback.
  it('returns null for a kind it does not describe', () => {
    expect(fieldsFor('something-new')).toBeNull()
  })
})

describe('getAt / setAt', () => {
  it('reads a nested path', () => {
    expect(getAt({dns: {query_name: 'example.com'}}, 'dns.query_name')).toBe('example.com')
    expect(getAt({}, 'dns.query_name')).toBeUndefined()
    expect(getAt(undefined, 'target')).toBeUndefined()
  })

  it('sets a nested path, creating what it needs', () => {
    expect(setAt({}, 'dns.query_name', 'example.com')).toEqual({dns: {query_name: 'example.com'}})
  })

  it('does not mutate the input', () => {
    const original = {target: 'a', http: {method: 'GET'}}
    const updated = setAt(original, 'http.method', 'POST')

    expect(original.http.method).toBe('GET')
    expect(updated.http.method).toBe('POST')
  })

  // An emptied field is removed rather than stored as "", so the spec does not
  // fill up with blank keys that then need explaining in the YAML view.
  it('removes a key when the value is cleared', () => {
    expect(setAt({target: 'a', http: {method: 'GET'}}, 'http.method', '')).toEqual({
      target: 'a',
      http: {},
    })
  })

  it('leaves sibling values alone', () => {
    const updated = setAt({target: 'a', dns: {query_name: 'x', preferred_ip_protocol: 'ip4'}}, 'dns.query_name', 'y')

    expect(updated.target).toBe('a')
    expect(updated.dns.preferred_ip_protocol).toBe('ip4')
  })
})

describe('templateFor', () => {
  it('seeds a runnable shape for the configured kinds', () => {
    expect(templateFor('http').target).toBe('')
    expect(templateFor('http').http.method).toBe('GET')
    expect(templateFor('tcp').target).toBe('')
  })

  // urthctl gets blackbox's defaults for free because those types implement
  // UnmarshalYAML; JSON posted by this UI does not. Without the fallback a probe
  // resolves ip6 only and a `localhost` target fails to resolve.
  it('seeds the IP protocol fallback the JSON path does not get for free', () => {
    expect(templateFor('tcp').tcp.IPProtocolFallback).toBe(true)
    expect(templateFor('icmp').icmp.IPProtocolFallback).toBe(true)
    expect(templateFor('dns').dns.IPProtocolFallback).toBe(true)
  })

  it('seeds a script for the scripted kinds', () => {
    expect(templateFor('rest').script).toContain('GET ')
    expect(templateFor('puppeteer').script).toContain('page.goto')
  })

  it('falls back to an empty spec for a kind it does not know', () => {
    expect(templateFor('something-new')).toEqual({})
  })
})
