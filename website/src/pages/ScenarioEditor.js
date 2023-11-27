import React, {useCallback} from 'react'
import styled from '@emotion/styled'
import {useParams} from 'react-router-dom'
import {useDispatch, useSelector} from 'react-redux'
import fetchScenario from '../actions/fetchScenario.js'
import ErrorInlay from '../components/ErrorInlay.js'
import SpinnerInlay from '../components/SpinnerInlay.js'
import Panel from '../components/Panel.js'
import FormGroup from '../components/FormGroup.js'
import FormLabel from '../components/FormLabel.js'
import FormControl from '../components/FormControl.js'
import FormGroupError from '../components/FormGroupError.js'
import {validateMaxLength, validateNotEmpty} from '../utils/validators.js'


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

const FormContainer = styled.div`
  display: flex;
  flex-direction: column;
  gap: 1rem;
`

const validateName = (...args) => validateNotEmpty(...args) || validateMaxLength(32)(...args)

const validateDescription = validateMaxLength(128)

const ScenarioEditor = () => {
  const {scenarioId} = useParams()

  const [isNew, setIsNew] = React.useState(false)

  const [name, setName] = React.useState('')
  const [description, setDescription] = React.useState('')

  const handleNameChange = useCallback((e) => {
    setName(e.target.value)
  }, [])

  const handleDescriptionChange = useCallback((e) => {
    setDescription(e.target.value)
  }, [])

  const {id, fetching, response, error} = useSelector(s => s.scenario)

  const dispatch = useDispatch()

  React.useEffect(() => {
    if (scenarioId === 'new') {
      setIsNew(true)
    } else {
      dispatch(fetchScenario(scenarioId))
    }
  }, [scenarioId])

  React.useEffect(() => {
    if (response) {
      setName(response.name)
      setDescription(response.description)
    }
  }, [response])

  if (!isNew && id !== scenarioId) {
    return null
  }

  if (error) {
    return <ErrorInlay message={"Error fetching scenarios"} details={error.message || ""}/>
  }

  if (fetching) {
    return <SpinnerInlay/>
  }

  return (
    <PageContainer>
      <PagePanel>
        <h3>Edit Scenario</h3>
        <FormContainer>
          <FormGroup controlId="scenario-name" onValidate={validateName}>
            <FormLabel required>Name</FormLabel>
            <FormControl type="text" value={name} onChange={handleNameChange}/>
            <FormGroupError/>
          </FormGroup>
          <FormGroup controlId="scenario-description" onValidate={validateDescription}>
            <FormLabel>Description</FormLabel>
            <FormControl as="textarea" rows="5" value={description} onChange={handleDescriptionChange}/>
            <FormGroupError/>
          </FormGroup>
        </FormContainer>
        {/*<p>You are here:</p>*/}
        {/*<p>Scenario {scenarioId} editor</p>*/}
        {/*<pre>{JSON.stringify(response, null, 2)}</pre>*/}
      </PagePanel>
    </PageContainer>
  )
}

export default ScenarioEditor
