import React, {useCallback, useEffect, useState} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import {useDispatch, useSelector} from 'react-redux'
import fetchArtifactContent from '../actions/fetchArtifactContent.js'
import Button from './Button.js'
import DataClassBadge from './DataClassBadge.jsx'
import SpinnerInlay from './SpinnerInlay.jsx'
import ErrorInlay from './ErrorInlay.jsx'
import TextSpan from './TextSpan.js'
import {LabelArtifact, dataClassOf, mayContainSecrets} from '../utils/labels.js'

const Container = styled.div`
  border: 1px solid ${(props) => props.theme.color.neutral[props.theme.dark ? 800 : 200]};
  border-radius: 0.5rem;
  margin-bottom: 0.75rem;
  overflow: hidden;
`

const Header = styled.div`
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.75rem;
  background-color: ${(props) => props.theme.color.neutral[props.theme.dark ? 900 : 50]};
`

const Title = styled.div`
  flex-grow: 1;
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 0.5rem;
`

const Content = styled.pre`
  margin: 0;
  padding: 0.75rem;
  max-height: 28rem;
  overflow: auto;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.8125rem;
  line-height: 1.4;
  white-space: pre-wrap;
  word-break: break-word;
  color: ${(props) => props.theme.color.neutral[props.theme.dark ? 200 : 800]};
`

const Warning = styled.div`
  padding: 0.5rem 0.75rem;
  background-color: ${(props) => props.theme.color.error[props.theme.dark ? 950 : 50]};
`

// Artifact kinds whose content is readable as text. Anything else (a screenshot,
// a compressed capture) is offered as a download rather than rendered inline.
const TEXTUAL_KINDS = ['log', 'har', 'metrics']

const isTextual = (artifact) => {
  const kind = artifact.metadata?.labels?.[LabelArtifact.Kind]
  return TEXTUAL_KINDS.includes(kind)
}

const ArtifactPanel = ({artifact, defaultOpen = false}) => {
  const dispatch = useDispatch()
  const name = artifact.metadata.name
  const kind = artifact.metadata?.labels?.[LabelArtifact.Kind] || 'artifact'
  const dataClass = dataClassOf(artifact)
  const sensitive = mayContainSecrets(artifact)
  const textual = isTextual(artifact)

  const [open, setOpen] = useState(defaultOpen && textual && !sensitive)
  const content = useSelector((s) => s.artifactContent[name]) || {}

  useEffect(() => {
    if (open && !content.response && !content.fetching && !content.error) {
      dispatch(fetchArtifactContent(name))
    }
  }, [open, name, content.response, content.fetching, content.error])

  const toggle = useCallback(() => setOpen((v) => !v), [])

  return (
    <Container>
      <Header>
        <Title>
          <TextSpan size="small" weight={500} level={2}>
            {kind}
          </TextSpan>
          <DataClassBadge dataClass={dataClass} />
          <TextSpan size="small" level={4}>
            {name}
          </TextSpan>
        </Title>
        {textual && (
          <Button size="small" color="neutral" onClick={toggle}>
            {open ? 'Hide' : 'View'}
          </Button>
        )}
        <Button size="small" color="neutral" as="a" href={`/api/v1/artifacts/${name}/content`} download>
          Download
        </Button>
      </Header>

      {open && sensitive && (
        <Warning>
          <TextSpan size="small" level={2} color="error">
            This artifact is a faithful capture and may contain credentials. Avoid pasting it into tickets or chat.
          </TextSpan>
        </Warning>
      )}

      {open && content.fetching && <SpinnerInlay />}
      {open && content.error && <ErrorInlay message="Could not load artifact" details={content.error.message || ''} />}
      {open && content.response !== undefined && <Content>{content.response}</Content>}
    </Container>
  )
}

ArtifactPanel.propTypes = {
  artifact: PropTypes.object.isRequired,
  defaultOpen: PropTypes.bool,
}

export default ArtifactPanel
