import {useEffect, useMemo, useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {ArrowLeft, Box, Download, Eye, ShieldAlert} from 'lucide-react'
import {Link, useParams} from 'react-router-dom'
import {api} from '../api'
import {artifactDataClass, artifactKind, artifactMime} from '../labels'
import {artifactResultName} from '../components/Data'
import {Button, Card, ErrorState, KeyValue, Loading, PageHeader, Status} from '../components/ui'
import {formatRelative, formatTimestamp} from '../utils'

function readBlobText(blob: Blob): Promise<string> {
  if (typeof blob.text === 'function') return blob.text()
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result ?? ''))
    reader.onerror = () => reject(reader.error)
    reader.readAsText(blob)
  })
}

function ContentPreview({blob, mime}: {blob: Blob; mime: string}) {
  const [text, setText] = useState('')
  const objectUrl = useMemo(() => mime.startsWith('image/') ? URL.createObjectURL(blob) : '', [blob, mime])
  useEffect(() => {
    if (mime.startsWith('text/') || mime.includes('json') || mime.includes('yaml') || mime.includes('javascript')) readBlobText(blob).then(setText)
    return () => {if (objectUrl) URL.revokeObjectURL(objectUrl)}
  }, [blob, mime, objectUrl])
  if (objectUrl) return <img className="artifact-preview-image" src={objectUrl} alt="Artifact preview" />
  if (mime.startsWith('text/') || mime.includes('json') || mime.includes('yaml') || mime.includes('javascript')) return <pre className="log-view">{text}</pre>
  return <div className="empty"><strong>No inline preview for {mime}</strong><p>Download the original artifact to inspect it with an appropriate tool.</p></div>
}

export function ArtifactDetailPage() {
  const {artifactName = ''} = useParams()
  const name = decodeURIComponent(artifactName)
  const artifact = useQuery({queryKey: ['artifact', name], queryFn: () => api.artifacts.get(name)})
  const classification = artifact.data ? artifactDataClass(artifact.data) : 'unknown'
  const unsafe = classification === 'unknown' || classification === 'secret-bearing'
  const [revealed, setRevealed] = useState(false)
  const content = useQuery({queryKey: ['artifact-content', name], queryFn: () => api.artifacts.content(name), enabled: Boolean(artifact.data) && (!unsafe || revealed)})
  if (artifact.isPending) return <div className="page"><Loading label="Loading artifact" /></div>
  if (artifact.isError) return <div className="page"><ErrorState title="Error loading artifact" error={artifact.error} retry={() => artifact.refetch()} /></div>
  const item = artifact.data
  const run = artifactResultName(item)
  const mime = artifactMime(item)
  function download() {
    if (!content.data) return
    const url = URL.createObjectURL(content.data.blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = item.metadata.name
    anchor.click()
    URL.revokeObjectURL(url)
  }
  return (
    <div className="page">
      <PageHeader
        eyebrow={<Link to="/artifacts"><ArrowLeft size={13} /> All artifacts</Link>}
        title={<><Box size={23} /><span className="mono">{item.metadata.name}</span></>}
        description={`${artifactKind(item)} · ${mime}`}
        actions={content.data && <Button tone="quiet" onClick={download}><Download size={16} /> Download</Button>}
      />
      <div className="detail-grid">
        <Card title="Artifact metadata" className="span-5">
          <KeyValue items={[
            {label: 'Kind', value: artifactKind(item)},
            {label: 'Data class', value: <Status value={classification} />},
            {label: 'MIME type', value: mime, mono: true},
            {label: 'Run', value: run ? <Link to={`/runs/${encodeURIComponent(run)}`}>{run}</Link> : 'unknown', mono: true},
            {label: 'Created', value: `${formatRelative(item.metadata.creationTimestamp)} · ${formatTimestamp(item.metadata.creationTimestamp)}`},
            {label: 'Expires', value: item.spec.expireTime || item.spec.expire_time ? formatTimestamp(item.spec.expireTime || item.spec.expire_time) : 'Pinned'},
            {label: 'UID', value: item.metadata.uid, mono: true},
          ]} />
        </Card>
        <Card title="Content handling" className="span-7">
          {unsafe ? <div className="security-callout"><ShieldAlert size={22} /><div><strong>{classification === 'unknown' ? 'Unclassified content' : 'This artifact may contain secrets'}</strong><p>{classification === 'unknown' ? 'The producer made no safety declaration. Treat the bytes as sensitive.' : 'This is a faithful capture and may include credentials or private request data.'}</p></div></div> : <div className="security-callout safe"><ShieldAlert size={22} /><div><strong>{classification} content</strong><p>The producer declared this content safe to inspect in the browser.</p></div></div>}
          {unsafe && !revealed && <Button tone="danger" onClick={() => setRevealed(true)}><Eye size={16} /> Reveal sensitive content</Button>}
        </Card>
      </div>
      {(!unsafe || revealed) && <Card title="Preview" meta={<span className="mono">{mime}</span>}>
        {content.isPending && <Loading label="Loading content" />}
        {content.isError && <ErrorState title="Content unavailable" error={content.error} retry={() => content.refetch()} />}
        {content.data && <ContentPreview blob={content.data.blob} mime={content.data.mime || mime} />}
      </Card>}
    </div>
  )
}
