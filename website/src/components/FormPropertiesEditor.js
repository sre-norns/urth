import React, {useCallback, useEffect, useState} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import Input from './Input.js'
import Button from './Button.js'
import FormGroupError from './FormGroupError.js'
import FormGroup from './FormGroup.js'
import FormControl from './FormControl.js'

const PropertyContainer = styled.div`
  display: flex;
  flex-direction: row;
  align-items: start;
  gap: 1rem;
`

const InputFormGroup = styled(FormGroup)`
  width: 100%;
`

const OperatorContainer = styled.div`
  padding-top: 0.3125rem;
`

const DeleteButton = styled(Button)`
  padding: 5px 0.75rem;
`

const AddButtonContainer = styled.div`
  display: flex;
  flex-direction: row-reverse;
`

const AddButton = styled(Button)`
  i {
    padding: 0 0.5rem 0 0;
  }
`

const validateNewPropertyName = (obj, propertyName, newPropertyName) => {
  if (newPropertyName === '') {
    return 'Name must not be empty.'
  }

  if (!/^[\p{L}_$]/u.test(newPropertyName)) {
    return 'Name must start with a Unicode letter, $ or _.'
  }

  if (/[\x00-\x1F]/.test(newPropertyName)) {
    return 'Name must not contain control characters.'
  }

  if (propertyName !== newPropertyName && obj.hasOwnProperty(newPropertyName)) {
    return 'Name already exists.'
  }
}

const renameProperty = (obj, oldName, newName) => {
  const newObj = {}
  for (let prop in obj) {
    if (obj.hasOwnProperty(prop)) {
      if (prop === oldName) {
        newObj[newName] = obj[prop]
      } else {
        newObj[prop] = obj[prop]
      }
    }
  }

  return newObj
}

const deleteProperty = (obj, name) => {
  const newObj = {}
  for (let prop in obj) {
    if (obj.hasOwnProperty(prop)) {
      if (prop !== name) {
        newObj[prop] = obj[prop]
      }
    }
  }
  return newObj
}

const findNewPropertyName = (obj) => {
  for (let i = 1; true; i++) {
    const name = `property${i}`
    if (!obj.hasOwnProperty(name)) {
      return name
    }
  }
}

const addNewProperty = (obj) => {
  const newPropertyName = findNewPropertyName(obj)
  return {...obj, [newPropertyName]: ''}
}

const PropertyEditor = ({controlId, obj, propertyName, onChange}) => {
  const [currentPropertyName, setCurrentPropertyName] = useState(propertyName)
  const [propertyNameError, setPropertyNameError] = useState(null)

  const handleValidatePropertyName = useCallback(
    (value) => {
      return validateNewPropertyName(obj, propertyName, value)
    },
    [obj, propertyName]
  )

  const handlePropertyNameChange = useCallback(
    (e) => {
      const newPropertyName = e.target.value
      setCurrentPropertyName(newPropertyName)
      const error = validateNewPropertyName(obj, propertyName, newPropertyName)
      setPropertyNameError(error)
      if (!error) {
        onChange && onChange(renameProperty(obj, propertyName, newPropertyName))
      }
    },
    [obj, propertyName, onChange]
  )

  const handleValueChange = useCallback(
    (e) => {
      onChange && onChange({...obj, [propertyName]: e.target.value})
    },
    [obj, propertyName, onChange]
  )

  const handleDelete = useCallback(() => {
    if (!propertyNameError) {
      onChange && onChange(deleteProperty(obj, propertyName))
    }
  }, [obj, propertyName, propertyNameError, onChange])

  useEffect(() => {
    setCurrentPropertyName(propertyName)
    setPropertyNameError(null)
  }, [propertyName])

  return (
    <PropertyContainer>
      <InputFormGroup
        controlId={(controlId && `${controlId}-name`) || undefined}
        onValidate={handleValidatePropertyName}
      >
        <FormControl type="text" value={currentPropertyName} onChange={handlePropertyNameChange} />
        <FormGroupError />
      </InputFormGroup>
      <OperatorContainer>=</OperatorContainer>
      <Input value={obj[propertyName]} onChange={handleValueChange} />
      <DeleteButton type="button" onClick={handleDelete} color="neutral">
        <i className="fi fi-trash"></i>
      </DeleteButton>
    </PropertyContainer>
  )
}

const EditorContainer = styled.div`
  display: flex;
  flex-direction: column;
  gap: 1rem;
`

const FormPropertiesEditor = ({controlId, value: obj, onChange}) => {
  const handleAddNewProperty = useCallback(() => {
    onChange && onChange(addNewProperty(obj))
  }, [obj, onChange])

  return (
    <EditorContainer>
      {Object.keys(obj).map((propertyName, index) => (
        <PropertyEditor
          key={index}
          controlId={(controlId && `${controlId}-${propertyName}`) || undefined}
          obj={obj}
          propertyName={propertyName}
          onChange={onChange}
        />
      ))}
      <AddButtonContainer>
        <AddButton type="button" onClick={handleAddNewProperty} color="neutral">
          <i className="fi fi-plus"></i>Add Property
        </AddButton>
      </AddButtonContainer>
    </EditorContainer>
  )
}

FormPropertiesEditor.propTypes = {
  controlId: PropTypes.string,
  value: PropTypes.object,
  onChange: PropTypes.func,
}

export default FormPropertiesEditor
