import {useEffect, useState} from 'react'
import {Badge, Button, Card, ErrorState, LoadingState} from '@sre-norns/components'
import {useAccountId, useIdentity, useSessionEpoch} from '@sre-norns/components/identity'
import {useQuery} from '@tanstack/react-query'
import {Link, useParams} from 'react-router-dom'
import {productProblem} from '../api/http'
import {artifact} from '../api/models'
import {resourcePath, useResource} from '../api/queries'
import {projectPath} from '../identity/links'
import {Collection, Heading} from './common'

export function Artifacts() {
  const {accountId = '', projectId = ''} = useParams()
  return <><Heading title="Artifacts" description="Logs, traces and files produced by scenario runs." />
    <Collection path={resourcePath('projects', projectId, 'artifacts')} schema={artifact} scope={projectId} title="Artifacts" columns={[
      {header: 'Artifact', cell: (a) => <Link to={`${projectPath(accountId, projectId)}/artifacts/${encodeURIComponent(a.metadata.name)}`}>{a.metadata.name}</Link>},
      {header: 'Kind', cell: (a) => a.spec.rel || a.metadata.labels?.['urth/artifact.kind'] || 'Unknown'},
      {header: 'Data class', cell: (a) => <Badge>{a.spec.dataClass || a.metadata.labels?.['urth/artifact.data-class'] || 'unknown'}</Badge>},
      {header: 'Run', cell: (a) => a.metadata.labels?.['urth/result.name'] || 'Unknown'},
      {header: 'Created', cell: (a) => a.metadata.creationTimestamp ? new Date(a.metadata.creationTimestamp).toLocaleString() : 'Unknown'},
    ]} /></>
}
function Preview({blob, mime}: {blob: Blob; mime: string}) {
  const [text, setText] = useState('')
  const [url, setURL] = useState('')
  // Only inert raster formats get an image preview. HTML/SVG never execute here.
  const image = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'].includes(mime.split(';')[0].trim())
  useEffect(() => {
    let gone = false
    if (image) {
      const url = URL.createObjectURL(blob); setURL(url)
      return () => URL.revokeObjectURL(url)
    }
    const reader = new FileReader()
    reader.onload = () => {if (!gone) setText(String(reader.result ?? ''))}
    reader.readAsText(blob.slice(0, 1_000_000))
    return () => {gone = true; if (reader.readyState === FileReader.LOADING) reader.abort()}
  }, [blob, image])
  if (image) return <img className="artifact-image" src={url} alt="Artifact preview" />
  if (/text\/|json|yaml|javascript|xml/.test(mime)) return <><pre className="run-log" tabIndex={0}>{text}</pre>{blob.size > 1_000_000 && <p>Preview limited to the first 1 MB. Download to inspect the complete file.</p>}</>
  return <p>No preview is available for {mime}. Download the artifact to inspect it.</p>
}
export function ArtifactDetail() {
  const {accountId = '', projectId = '', artifactName = ''} = useParams()
  const {session, queryClient} = useIdentity()
  const account = useAccountId()
  const epoch = useSessionEpoch()
  const path = resourcePath('projects', projectId, 'artifacts', artifactName)
  const query = useResource(path, artifact, projectId)
  const [revealed, setRevealed] = useState(false)
  const classification = query.data?.data.spec.dataClass || query.data?.data.metadata.labels?.['urth/artifact.data-class'] || 'unknown'
  const unsafe = !['clean', 'redacted'].includes(classification)
  const content = useQuery({queryKey: [account, projectId, path, 'content', epoch], enabled: Boolean(query.data) && (!unsafe || revealed), queryFn: async ({signal}) => {
    const response = await session.authenticatedFetch(`${path}/content`, {signal})
    if (!response.ok) throw productProblem(response.status, await response.json().catch(() => null))
    return {blob: await response.blob(), mime: response.headers.get('Content-Type') || 'application/octet-stream'}
  }}, queryClient)
  if (query.isError) return <ErrorState error={query.error} />
  if (!query.data) return <LoadingState />
  const a = query.data.data
  function download() {
    if (!content.data) return
    const url = URL.createObjectURL(content.data.blob)
    const link = document.createElement('a'); link.href = url; link.download = a.metadata.name; link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  return <>
    <Heading title={a.metadata.name} description={a.spec.rel || 'Artifact'} />
    <Card><h2>Content handling</h2><Badge>{classification}</Badge><p>{unsafe ? 'This artifact may contain credentials or private data. Reveal it only when you intend to inspect sensitive content.' : 'The producer classified this content for inspection.'}</p>
      {unsafe && !revealed && <Button variant="danger" onClick={() => setRevealed(true)}>Reveal sensitive content</Button>}
      {a.metadata.labels?.['urth/result.name'] && <p><Link to={`${projectPath(accountId, projectId)}/runs/${encodeURIComponent(a.metadata.labels['urth/result.name'])}`}>Source run</Link></p>}
    </Card>
    {(!unsafe || revealed) && <Card><h2>Preview</h2>
      {content.isPending && <LoadingState />}{content.isError && <ErrorState error={content.error} retry={() => void content.refetch()} />}
      {content.data && <><Button variant="secondary" onClick={download}>Download artifact</Button><Preview {...content.data} /></>}
    </Card>}
  </>
}
