import React, { useCallback, useEffect, useMemo, useState } from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import { useDispatch, useSelector } from 'react-redux'
import fetchProbKinds from '../actions/fetchProbKinds.js'
import FormGroup from './FormGroup.jsx'
import FormLabel from './FormLabel.jsx'
import FormControl from './FormControl.jsx'
import TextSpan, { TextDiv } from './TextSpan.js'
import { fieldsFor, getAt, isScriptKind, setAt, templateFor } from '../utils/probSpec.js'

const Row = styled.div`
  display: flex;
  flex-direction: column;
  gap: 1rem;
`

const KindSelect = styled.select`
  display: block;
  padding: 0.125rem 0.5rem;
  font-size: 1rem;
  line-height: 1.5rem;
  border-radius: 0.5rem;
  color: ${(props) => props.theme.color.neutral[props.theme.dark ? 300 : 700]};
  background-color: ${(props) => props.theme.color.neutral[props.theme.dark ? 950 : 50]};
  border: 1px solid ${(props) => props.theme.color.neutral[props.theme.dark ? 300 : 700]};
`

const ScriptArea = styled(FormControl)`
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.8125rem;
`

const AdvancedToggle = styled.button`
  background: none;
  border: none;
  padding: 0;
  cursor: pointer;
  font: inherit;
  font-size: 0.8125rem;
  text-decoration: underline;
  color: ${(props) => props.theme.color.neutral[props.theme.dark ? 300 : 700]};
`

// A prob's spec is rendered as JSON in the fallback rather than YAML: it is what
// the API actually exchanges, it round-trips without another dependency, and an
// author dropping to this view is already reading the manifest shape.
const formatSpec = (spec) => JSON.stringify(spec ?? {}, null, 2)

const ProbEditor = ({ value, onChange, readOnly = false }) => {
  const dispatch = useDispatch()
  const { response, fetching, error } = useSelector((s) => s.probKinds)

  const kinds = useMemo(() => response?.data || [], [response])
  const kind = value?.kind || ''
  const kindInfo = useMemo(() => kinds.find((k) => k.kind === kind), [kinds, kind])
  const fields = fieldsFor(kind)

  const [advanced, setAdvanced] = useState(false)
  const [rawSpec, setRawSpec] = useState(() => formatSpec(value?.spec))
  const [rawError, setRawError] = useState(null)

  useEffect(() => {
    if (!response && !fetching && !error) {
      dispatch(fetchProbKinds())
    }
  }, [response, fetching, error])

  // Keep the raw view in step while the fields are the ones being edited.
  useEffect(() => {
    if (!advanced) {
      setRawSpec(formatSpec(value?.spec))
      setRawError(null)
    }
  }, [value?.spec, advanced])

  const handleKindChange = useCallback(
    (e) => {
      const nextKind = e.target.value
      // Switching kind replaces the spec: the shapes are unrelated, and carrying
      // the old one over would leave fields that mean nothing to the new kind.
      onChange({ ...value, kind: nextKind, spec: templateFor(nextKind) })
    },
    [value, onChange]
  )

  const handleFieldChange = useCallback(
    (path) => (e) => onChange({ ...value, spec: setAt(value?.spec, path, e.target.value) }),
    [value, onChange]
  )

  const handleScriptChange = useCallback(
    (e) => onChange({ ...value, spec: setAt(value?.spec, 'script', e.target.value) }),
    [value, onChange]
  )

  const handleRawChange = useCallback(
    (e) => {
      const text = e.target.value
      setRawSpec(text)

      try {
        onChange({ ...value, spec: JSON.parse(text) })
        setRawError(null)
      } catch (parseError) {
        // Keep the text as typed and report it, rather than discarding what the
        // author is halfway through writing.
        setRawError(parseError.message)
      }
    },
    [value, onChange]
  )

  const scripted = isScriptKind(kindInfo)

  return (
    <Row>
      <FormGroup controlId="prob-kind">
        <FormLabel required>Type</FormLabel>
        {readOnly ? (
          <div>{kind || 'none'}</div>
        ) : (
          <KindSelect id="prob-kind" value={kind} onChange={handleKindChange}>
            <option value="">Select a prob type…</option>
            {kinds.map((k) => (
              <option key={k.kind} value={k.kind}>
                {k.kind}
              </option>
            ))}
          </KindSelect>
        )}
        {error && (
          <TextDiv size="small" level={3} color="warning">
            Could not load the list of prob types from the server.
          </TextDiv>
        )}
      </FormGroup>

      {kind && scripted && (
        <FormGroup controlId="prob-script">
          <FormLabel>Script</FormLabel>
          {readOnly ? (
            <ScriptArea as="textarea" rows="10" value={getAt(value?.spec, 'script') || ''} readOnly />
          ) : (
            <ScriptArea
              as="textarea"
              rows="10"
              value={getAt(value?.spec, 'script') || ''}
              onChange={handleScriptChange}
            />
          )}
        </FormGroup>
      )}

      {kind &&
        !scripted &&
        fields?.map((field) => (
          <FormGroup key={field.path} controlId={`prob-${field.path}`}>
            <FormLabel required={field.required}>{field.label}</FormLabel>
            {readOnly ? (
              <div>{getAt(value?.spec, field.path) || '—'}</div>
            ) : (
              <FormControl
                type="text"
                placeholder={field.placeholder}
                value={getAt(value?.spec, field.path) || ''}
                onChange={handleFieldChange(field.path)}
              />
            )}
          </FormGroup>
        ))}

      {/* A kind the server knows but this UI has no fields for is still
          editable, rather than being unreachable until the UI catches up. */}
      {kind && !scripted && !fields && (
        <TextDiv size="small" level={3}>
          No form for <TextSpan weight={500}>{kind}</TextSpan> yet — edit its spec below.
        </TextDiv>
      )}

      {kind && !readOnly && (
        <div>
          <AdvancedToggle type="button" onClick={() => setAdvanced((v) => !v)}>
            {advanced ? 'Hide raw spec' : 'Edit raw spec'}
          </AdvancedToggle>
        </div>
      )}

      {kind && (advanced || (!scripted && !fields)) && (
        <FormGroup controlId="prob-spec">
          <FormLabel>Spec</FormLabel>
          <ScriptArea
            as="textarea"
            rows="12"
            value={rawSpec}
            onChange={handleRawChange}
            readOnly={readOnly}
          />
          {rawError && (
            <TextDiv size="small" level={3} color="error">
              {rawError}
            </TextDiv>
          )}
        </FormGroup>
      )}
    </Row>
  )
}

ProbEditor.propTypes = {
  value: PropTypes.shape({
    kind: PropTypes.string,
    timeout: PropTypes.any,
    spec: PropTypes.any,
  }),
  onChange: PropTypes.func.isRequired,
  readOnly: PropTypes.bool,
}

export default ProbEditor
