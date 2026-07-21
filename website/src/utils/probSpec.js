// Knowledge about the shape of each prob kind's spec.
//
// The kinds themselves come from the server's registry -- this only describes
// how to *edit* the ones worth giving fields to. A kind the server reports that
// is not described here still works: it falls back to the YAML editor, so the UI
// does not have to be changed in step with the server to stay usable.

// Kinds whose spec is a script rather than a set of options. Identified by the
// content type the server reports, so a new scripted kind is handled without
// naming it here.
const SCRIPT_CONTENT_TYPES = ['text/javascript', 'text/x-python', 'application/http', 'application/json']

export const isScriptKind = (kindInfo) => SCRIPT_CONTENT_TYPES.includes(kindInfo?.contentType)

// Fields offered for the configured kinds. `target` is common to all of them;
// the rest are the options worth setting without dropping into YAML.
//
// Nested paths are written as `dns.query_name`, matching the manifest, because
// that is what someone reading the YAML fallback will see.
export const PROB_FIELDS = {
  http: [
    { path: 'target', label: 'Target URL', placeholder: 'https://example.com/health', required: true },
    { path: 'http.method', label: 'Method', placeholder: 'GET' },
  ],
  tcp: [{ path: 'target', label: 'Target host:port', placeholder: 'example.com:443', required: true }],
  icmp: [{ path: 'target', label: 'Target host', placeholder: 'example.com', required: true }],
  grpc: [{ path: 'target', label: 'Target host:port', placeholder: 'example.com:443', required: true }],
  dns: [
    { path: 'target', label: 'DNS server', placeholder: '8.8.8.8', required: true },
    { path: 'dns.query_name', label: 'Query name', placeholder: 'example.com' },
  ],
}

export const fieldsFor = (kind) => PROB_FIELDS[kind] || null

export const getAt = (obj, path) =>
  path.split('.').reduce((acc, key) => (acc == null ? undefined : acc[key]), obj)

// Returns a copy with the value set, creating intermediate objects as needed.
// An emptied field is removed rather than written as "", so a spec does not
// accumulate blank keys that then have to be explained in the YAML view.
export const setAt = (obj, path, value) => {
  const keys = path.split('.')
  const next = { ...(obj || {}) }

  let cursor = next
  for (let i = 0; i < keys.length - 1; i += 1) {
    cursor[keys[i]] = { ...(cursor[keys[i]] || {}) }
    cursor = cursor[keys[i]]
  }

  const last = keys[keys.length - 1]
  if (value === '' || value === undefined || value === null) {
    delete cursor[last]
  } else {
    cursor[last] = value
  }

  return next
}

// A starting spec for a kind, so a new scenario is runnable as soon as the
// target is filled in rather than requiring the author to know the shape.
export const templateFor = (kind) => {
  switch (kind) {
    case 'http':
      return { target: '', http: { method: 'GET', IPProtocolFallback: true } }
    // The IPProtocolFallback below is a stopgap, and deliberately spelled with
    // Go field names because that is what goes over the wire.
    //
    // The blackbox_exporter config types these probs use implement UnmarshalYAML
    // but not UnmarshalJSON, so their defaults are applied when urthctl parses a
    // YAML manifest and *not* when this UI posts JSON. Without it a probe
    // resolves ip6 only, and a target like `localhost` fails on a host with no
    // IPv6 address. The real fix is for the server to apply prober defaults on
    // create -- see TODO.md -- after which this can go.
    case 'tcp':
    case 'icmp':
    case 'grpc':
      return { target: '', [kind]: { IPProtocolFallback: true } }
    case 'dns':
      return {
        target: '',
        dns: { query_name: '', preferred_ip_protocol: 'ip4', IPProtocolFallback: true },
      }
    case 'rest':
      return { script: 'GET https://example.com/health\n' }
    case 'puppeteer':
      return { script: "// await page.goto('https://example.com')\n" }
    case 'pypuppeteer':
      return { script: "# await page.goto('https://example.com')\n" }
    default:
      return {}
  }
}
