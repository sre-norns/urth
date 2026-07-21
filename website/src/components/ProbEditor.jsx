import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import { useDispatch, useSelector } from 'react-redux'
import { parse as parseYaml, stringify as stringifyYaml } from 'yaml'
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

// The caret is drawn here because `appearance: none` -- needed to style the
// control at all -- removes the native one, leaving something that does not read
// as a dropdown.
const KindSelect = styled.select`
  display: block;
  width: 100%;
  padding: 0.125rem 2rem 0.125rem 0.5rem;
  font-size: 1rem;
  line-height: 1.5rem;
  border-radius: 0.5rem;
  color: ${(props) => props.theme.color.neutral[props.theme.dark ? 300 : 700]};
  background-color: ${(props) => props.theme.color.neutral[props.theme.dark ? 950 : 50]};
  border: 1px solid ${(props) => props.theme.color.neutral[props.theme.dark ? 300 : 700]};

  appearance: none;
  background-image: linear-gradient(45deg, transparent 50%, currentColor 50%),
    linear-gradient(135deg, currentColor 50%, transparent 50%);
  background-position:
    calc(100% - 18px) calc(50% + 2px),
    calc(100% - 13px) calc(50% + 2px);
  background-size: 5px 5px;
  background-repeat: no-repeat;
`

const ScriptArea = styled(FormControl)`
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.8125rem;
`

const TabRow = styled.div`
  display: flex;
  flex-direction: row;
  gap: 0.25rem;
  border-bottom: 1px solid ${(props) => props.theme.color.neutral[props.theme.dark ? 800 : 200]};
`

const Tab = styled.button`
  background: none;
  border: none;
  border-bottom: 2px solid
    ${(props) =>
      props.selected ? props.theme.color.secondary[props.theme.dark ? 400 : 600] : 'transparent'};
  margin-bottom: -1px;
  padding: 0.25rem 0.75rem;
  cursor: pointer;
  font: inherit;
  font-size: 0.875rem;
  font-weight: ${(props) => (props.selected ? 600 : 400)};
  color: ${(props) => props.theme.color.neutral[props.theme.dark ? 200 : 800]};
`

const Mode = { Form: 'form', Yaml: 'yaml' }

// The raw view is YAML because that is the manifest format: what is shown here
// matches what an author would write for `urthctl apply`.
const formatSpec = (spec) => stringifyYaml(spec ?? {})

const ProbEditor = ({ value, onChange, readOnly = false }) => {
  const dispatch = useDispatch()
  const { response, fetching, error } = useSelector((s) => s.probKinds)

  const kinds = useMemo(() => response?.data || [], [response])
  const kind = value?.kind || ''
  const kindInfo = useMemo(() => kinds.find((k) => k.kind === kind), [kinds, kind])
  const fields = fieldsFor(kind)
  const scripted = isScriptKind(kindInfo)

  // Form or YAML, never both. Two editors over one value means keeping them in
  // step, and a half-written YAML document has no correct projection back onto
  // the fields -- so the choice is made once and the other view is not rendered.
  const [mode, setMode] = useState(Mode.Form)
  const [rawSpec, setRawSpec] = useState(() => formatSpec(value?.spec))
  const [rawError, setRawError] = useState(null)

  // Tracks the spec this component last emitted, so an edit made here does not
  // bounce back and overwrite what is being typed, while a change from anywhere
  // else -- switching kind, loading a scenario -- does refresh the text.
  const lastEmitted = useRef(value?.spec)

  useEffect(() => {
    if (!response && !fetching && !error) {
      dispatch(fetchProbKinds())
    }
  }, [response, fetching, error])

  useEffect(() => {
    if (value?.spec !== lastEmitted.current) {
      setRawSpec(formatSpec(value?.spec))
      setRawError(null)
      lastEmitted.current = value?.spec
    }
  }, [value?.spec])

  const emit = useCallback(
    (next) => {
      lastEmitted.current = next.spec
      onChange(next)
    },
    [onChange]
  )

  const handleKindChange = useCallback(
    (e) => {
      const nextKind = e.target.value
      // Switching kind replaces the spec: the shapes are unrelated, and carrying
      // the old one over would leave fields meaningless to the new kind. The raw
      // text is reset with it, which it was not when both views were on screen.
      const spec = templateFor(nextKind)

      setRawSpec(formatSpec(spec))
      setRawError(null)
      lastEmitted.current = spec
      onChange({ ...value, kind: nextKind, spec })
    },
    [value, onChange]
  )

  const handleFieldChange = useCallback(
    (path) => (e) => emit({ ...value, spec: setAt(value?.spec, path, e.target.value) }),
    [value, emit]
  )

  const handleScriptChange = useCallback(
    (e) => emit({ ...value, spec: setAt(value?.spec, 'script', e.target.value) }),
    [value, emit]
  )

  const handleRawChange = useCallback(
    (e) => {
      const text = e.target.value
      setRawSpec(text)

      try {
        const parsed = parseYaml(text)
        emit({ ...value, spec: parsed ?? {} })
        setRawError(null)
      } catch (parseError) {
        // Keep the text as typed and say what is wrong, rather than discarding
        // what the author is halfway through writing.
        setRawError(parseError.message)
      }
    },
    [value, emit]
  )

  // A kind this UI has no fields for is edited as YAML regardless of the tab,
  // rather than being unreachable until the UI catches up with the server.
  const showYaml = mode === Mode.Yaml || (!scripted && !fields)

  return (
    <Row>
      <FormGroup controlId="prob-kind">
        <FormLabel required>Type</FormLabel>
        {readOnly ? (
          <div>{kind || 'none'}</div>
        ) : (
          <KindSelect id="prob-kind" value={kind} onChange={handleKindChange}>
            {/* A placeholder, not a choice: disabled and hidden, so it shows on
                a new scenario but cannot be selected back afterwards. */}
            <option value="" disabled hidden>
              Select a prob type…
            </option>
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

      {kind && !readOnly && fields && !scripted && (
        <TabRow>
          <Tab type="button" selected={mode === Mode.Form} onClick={() => setMode(Mode.Form)}>
            Form
          </Tab>
          <Tab type="button" selected={mode === Mode.Yaml} onClick={() => setMode(Mode.Yaml)}>
            YAML
          </Tab>
        </TabRow>
      )}

      {kind && scripted && (
        <FormGroup controlId="prob-script">
          <FormLabel>Script</FormLabel>
          <ScriptArea
            as="textarea"
            rows="12"
            value={getAt(value?.spec, 'script') || ''}
            onChange={handleScriptChange}
            readOnly={readOnly}
          />
        </FormGroup>
      )}

      {kind &&
        !scripted &&
        !showYaml &&
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

      {kind && !scripted && !fields && (
        <TextDiv size="small" level={3}>
          No form for <TextSpan weight={500}>{kind}</TextSpan> yet — edit its spec as YAML.
        </TextDiv>
      )}

      {kind && !scripted && showYaml && (
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
