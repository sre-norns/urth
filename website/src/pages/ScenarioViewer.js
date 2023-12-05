import React, {useCallback} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import {useParams} from 'react-router-dom'
import {useDispatch, useSelector} from 'react-redux'
import fetchScenario from '../actions/fetchScenario.js'
import ErrorInlay from '../components/ErrorInlay.js'
import SpinnerInlay from '../components/SpinnerInlay.js'
import Panel from '../components/Panel.js'
import Form from '../components/Form.js'
import FormGroup from '../components/FormGroup.js'
import FormLabel from '../components/FormLabel.js'
import FormControl from '../components/FormControl.js'
import FormGroupError from '../components/FormGroupError.js'
import {validateMaxLength, validateNotEmpty} from '../utils/validators.js'
import Button from '../components/Button.js'
import routed from '../utils/routed.js'
import updateScenario from '../actions/updateScenario.js'
import ObjectCapsules from '../components/ObjectCapsules.js'


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

const PageForm = styled(Form)`
  display: flex;
  flex-direction: column;
  gap: 1rem;
`

const EditButton = routed(Button.withComponent('a'), true)

const validateName = (...args) => validateNotEmpty(...args) || validateMaxLength(32)(...args)

const validateDescription = validateMaxLength(128)

const ScenarioViewer = ({edit = false}) => {
  const {scenarioId} = useParams()

  const [isNew, setIsNew] = React.useState(false)

  const formRef = React.useRef(null)
  const [isValid, setIsValid] = React.useState(true)

  const [name, setName] = React.useState('')
  const [description, setDescription] = React.useState('')

  const handleNameChange = useCallback((e) => {
    setName(e.target.value)
  }, [])

  const handleDescriptionChange = useCallback((e) => {
    setDescription(e.target.value)
  }, [])

  const {id, fetching, updating, response, error} = useSelector(s => s.scenario)

  const dispatch = useDispatch()

  const handleSave = useCallback(() => {
    if (!formRef.current || !formRef.current.validate()) {
      return
    }

    dispatch(updateScenario(scenarioId, response.metadata.version, {
      kind: response.kind,
      metadata: {
        name,
        labels: response.metadata.labels,
      },
      spec: {
        description,
        requirements: response.spec.requirements,
        active: response.spec.active,
        prob: response.spec.prob,
        },
    }))
  }, [name, description])

  React.useEffect(() => {
    if (scenarioId === 'new') {
      setIsNew(true)
    } else {
      dispatch(fetchScenario(scenarioId))
    }
  }, [scenarioId])

  React.useEffect(() => {
    if (response) {
      setName(response.metadata.name)
      setDescription(response.spec.description)
    }
  }, [response])

  if (!isNew && id !== scenarioId) {
    return null
  }

  if (error) {
    return <ErrorInlay message="Error" details={error.message || ""}/>
  }

  if (fetching || updating) {
    return <SpinnerInlay/>
  }

  const title = edit ? (isNew ? 'New Scenario' : `Edit Scenario`) : name

  return (
    <PageContainer>
      <PagePanel>
        <HeaderPanel level={2}>
          <h3>{title}</h3>
          {!edit && <EditButton href={`/scenarios/${scenarioId}/edit`}>
            <i className="fi fi-page-edit"></i>&nbsp;Edit</EditButton>}
          {edit && <Button onClick={handleSave} disabled={!isValid}><i className="fi fi-save"></i>&nbsp;Save</Button>}
        </HeaderPanel>
        <PageForm ref={formRef} onValidated={setIsValid}>
          {edit &&
            <FormGroup controlId="scenario-name" onValidate={validateName}>
              <FormLabel required>Name</FormLabel>
              <FormControl type="text" value={name} onChange={handleNameChange}/>
              <FormGroupError/>
            </FormGroup>
          }
          <FormGroup controlId="scenario-description" onValidate={validateDescription}>
            <FormLabel>Description</FormLabel>
            {edit && <FormControl as="textarea" rows="5" value={description} onChange={handleDescriptionChange}/>}
            {!edit && <div>{description}</div>}
            <FormGroupError/>
          </FormGroup>
          {!edit && Object.keys(response.metadata.labels || {}).length &&
            <FormGroup controlId="scenario-labels">
              <FormLabel>Labels</FormLabel>
              <ObjectCapsules value={response.metadata.labels}/>
            </FormGroup>
          }
        </PageForm>
      </PagePanel>
    </PageContainer>
  )
}

ScenarioViewer.propTypes = {
  edit: PropTypes.bool,
}

export default ScenarioViewer
