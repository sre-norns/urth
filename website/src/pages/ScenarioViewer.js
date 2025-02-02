import React, { useCallback } from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import { useLocation } from 'wouter'
import { useDispatch, useSelector } from 'react-redux'
import { useTrackedState, useTracker } from '../utils/tracking.js'
import { validateMaxLength, validateNotEmpty } from '../utils/validators.js'
import { routed } from '../utils/routing.js'
import { isEmpty } from '../utils/objects.js'
import fetchScenario from '../actions/fetchScenario.js'
import createScenario from '../actions/createScenario.js'
import updateScenario from '../actions/updateScenario.js'
import deleteScenario from '../actions/deleteScenario.js'
import ErrorInlay from '../components/ErrorInlay.js'
import SpinnerInlay from '../components/SpinnerInlay.js'
import Panel from '../components/Panel.js'
import Form from '../components/Form.js'
import FormGroup from '../components/FormGroup.js'
import FormLabel from '../components/FormLabel.js'
import FormControl from '../components/FormControl.js'
import FormGroupError from '../components/FormGroupError.js'
import Button from '../components/Button.js'
import ObjectCapsules from '../components/ObjectCapsules.js'
import FormSwitch from '../components/FormSwitch.js'
import FormPropertiesEditor from '../components/FormPropertiesEditor.js'
import Label from '../components/Label.js'
import PanelSplitter from '../components/PanelSplitter.js'

const PageContainer = styled.div`
  width: 100%;
  max-width: 1320px;
  margin-left: auto;
  margin-right: auto;
`

const PagePanel = styled(Panel)`
  margin: 1.5rem;
`

const StyledLabel = styled(FormLabel)`
  font-weight: normal;
  text-decoration: underline;
`

const HeaderPanel = styled(Panel)`
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 1rem;
  border-bottom-left-radius: 0;
  border-bottom-right-radius: 0;
  margin: -1rem -1rem 1rem -1rem;
  padding: 1rem 1rem 1rem 1rem;

  h3 {
    flex-grow: 1;
    margin: 0;

    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
`

const HeaderButton = styled(Button)`
  min-width: 96px;
  i {
    padding: 0 0.5rem 0 0;
  }
`

const EditButton = routed(HeaderButton.withComponent('a'), true)

const PageForm = styled(Form)`
  display: flex;
  flex-direction: column;
  gap: 1rem;
`

const HorizontalFormGroup = styled(FormGroup)`
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 1rem;
`

const HorizontalLabel = styled(FormLabel)`
  padding: 0;
  flex-grow: 1;
`

const DeleteButtonContainer = styled.div`
  display: flex;
  flex-direction: row-reverse;
`

const DeleteButton = styled(Button)`
  i {
    padding: 0 0.5rem 0 0;
  }
`

const validateName = (...args) => validateNotEmpty(...args) || validateMaxLength(32)(...args)

const validateDescription = validateMaxLength(128)

const ScenarioViewer = ({ scenarioId, edit = false }) => {
  const [location, setLocation] = useLocation()
  const [isNew, setIsNew] = React.useState(false)

  const formRef = React.useRef(null)
  const [isValid, setIsValid] = React.useState(true)

  const tracker = useTracker()
  const [name, setName] = useTrackedState(tracker, '')
  const [labels, setLabels] = useTrackedState(tracker, {})
  const [description, setDescription] = useTrackedState(tracker, '')
  const [active, setActive] = useTrackedState(tracker, false)

  const { id, fetching, creating, updating, deleting, response, error } = useSelector((s) => s.scenario)
  const dispatch = useDispatch()

  const handleResponse = useCallback(
    (response) => {
      if (response && response.metadata.name === scenarioId) {
        setName(response.metadata.name)
        setLabels(response.metadata.labels || {})
        setDescription(response.spec.description)
        setActive(response.spec.active)
        tracker.reset()
      }
    },
    [scenarioId]
  )

  const handleValidated = useCallback((isValid) => {
    setIsValid(isValid)
  }, [])

  const handleNameChange = useCallback((e) => {
    setName(e.target.value)
  }, [])

  const handleDescriptionChange = useCallback((e) => {
    setDescription(e.target.value)
  }, [])

  const handleActiveClick = useCallback((e) => {
    setActive(!e.target.checked)
  }, [])

  const handleCancel = useCallback(() => {
    if (isNew) {
      setLocation('/scenarios')
    } else {
      handleResponse(response)
      setLocation(`/scenarios/${scenarioId}`, { replace: true })
    }
  }, [isNew, response])

  const handleSave = useCallback(() => {
    if (!formRef.current || !formRef.current.validate()) {
      return
    }

    if (isNew) {
      dispatch(
        createScenario(
          {
            metadata: {
              name,
              labels,
            },
            spec: {
              description,
              active,
            },
          },
          (id) => {
            setIsNew(false)
            setLocation(`/scenarios/${id}`, { replace: true })
          }
        )
      )
    } else {
      dispatch(
        updateScenario(
          scenarioId,
          response.metadata.version,
          {
            // kind: response.kind,
            metadata: {
              name,
              labels,
            },
            spec: {
              ...response.spec,
              description,
              active,
              // requirements: response.spec.requirements,
              // active: response.spec.active,
              // prob: response.spec.prob,
            },
          },
          () => setLocation(`/scenarios/${scenarioId}`, { replace: true })
        )
      )
    }
  }, [isNew, name, labels, description, active, response])

  const handleSubmit = useCallback(
    (e) => {
      e.preventDefault()
      if (edit) {
        handleSave()
      }
    },
    [edit, handleSave]
  )

  const handleDelete = useCallback(() => {
    if (confirm('Are you sure you want to delete this scenario?')) {
      dispatch(deleteScenario(scenarioId, response.metadata.version, () => setLocation('/scenarios')))
    }
  }, [scenarioId, response])

  React.useEffect(() => {
    if (scenarioId === 'new') {
      setIsNew(true)
      setName('')
      setLabels({})
      setDescription('')
      setActive(false)
      tracker.reset()
    } else {
      dispatch(fetchScenario(scenarioId))
    }
  }, [scenarioId])

  React.useEffect(() => {
    handleResponse(response)
  }, [response])

  if (!isNew && id !== scenarioId) {
    return null
  }

  if (error) {
    return <ErrorInlay message="Error" details={error.message || ''} />
  }

  if (fetching || creating || updating || deleting) {
    return <SpinnerInlay />
  }

  const title = edit ? (isNew ? 'New Scenario' : name) : name

  return (
    <PageContainer>
      <PagePanel>
        <HeaderPanel level={2}>
          <h3>{title}</h3>
          {!edit && (
            <EditButton href={`/scenarios/${scenarioId}/edit`}>
              <i className="fi fi-page-edit"></i> Edit
            </EditButton>
          )}
          {edit && (
            <HeaderButton onClick={handleCancel} color="neutral">
              <i className="fi fi-trash"></i> Cancel
            </HeaderButton>
          )}
          {edit && (
            <HeaderButton onClick={handleSave} disabled={!isValid || !tracker.changed}>
              <i className="fi fi-save"></i> Save
            </HeaderButton>
          )}
        </HeaderPanel>
        <PageForm ref={formRef} onSubmit={handleSubmit} onValidated={handleValidated}>
          {edit && isNew && (
            <FormGroup controlId="scenario-name" onValidate={validateName}>
              <FormLabel required>Name</FormLabel>
              <FormControl type="text" value={name} onChange={handleNameChange} />
              <FormGroupError />
            </FormGroup>
          )}
          <FormGroup controlId="scenario-description" onValidate={validateDescription}>
            <FormLabel>Description</FormLabel>
            {(edit && (
              <>
                <FormControl as="textarea" rows="5" value={description} onChange={handleDescriptionChange} />
                <FormGroupError />
              </>
            )) || <div>{description}</div>}
          </FormGroup>
          {!edit && !isEmpty(response?.metadata?.labels) && (
            <FormGroup controlId="scenario-labels">
              <FormLabel>Labels</FormLabel>
              <ObjectCapsules value={response.metadata.labels} />
            </FormGroup>
          )}
          {edit && (
            <div>
              <Label>Labels</Label>
              <FormPropertiesEditor controlId="scenario-labels" value={labels} onChange={setLabels} />
            </div>
          )}
          <HorizontalFormGroup controlId="scenario-active">
            <HorizontalLabel>Active</HorizontalLabel>
            <FormSwitch checked={active} readOnly={!edit} onClick={(edit && handleActiveClick) || null} />
          </HorizontalFormGroup>
        </PageForm>
        {edit && !isNew && (
          <>
            <PanelSplitter level={2} />
            <DeleteButtonContainer>
              <DeleteButton onClick={handleDelete} color="error">
                <i className="fi fi-trash"></i> Delete
              </DeleteButton>
            </DeleteButtonContainer>
          </>
        )}
      </PagePanel>
    </PageContainer>
  )
}

ScenarioViewer.propTypes = {
  scenarioId: PropTypes.string.isRequired,
  edit: PropTypes.bool,
}

export default ScenarioViewer
