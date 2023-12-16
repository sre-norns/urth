import React, { useCallback, useState } from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import Input from './Input.js'
import Button from './Button.js'

const PropertyContainer = styled.div`
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 1rem;
`

const DeleteButton = styled(Button)`
  padding: 5px 0.75rem;
`

const validatePropertyName = (propertyName) => {
  if (propertyName === '') {
    return 'Name must not be empty.'
  }

  if (/[\x00-\x1F]/.test(propertyName)) {
    return 'Name must not contain control characters.'
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
  return newObj;
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
  return newObj;
}

const PropertyEditor = ({obj, propertyName, onChange}) => {
  const [currentPropertyName, setCurrentPropertyName] = useState(propertyName)
  const [propertyNameError, setPropertyNameError] = useState(null)

  const handlePropertyNameChange = useCallback((e) => {
    const newPropertyName = e.target.value
    setCurrentPropertyName(newPropertyName)
    const error = validatePropertyName(newPropertyName)
    setPropertyNameError(error)
    if (!error) {
      onChange && onChange(renameProperty(obj, propertyName, newPropertyName))
    }
  }, [obj, propertyName, onChange])

  const handleValueChange = useCallback((e) => {
    onChange && onChange({...obj, [propertyName]: e.target.value})
  }, [obj, propertyName, onChange])

  const handleDelete = useCallback(() => {
    if (!propertyNameError) {
      onChange && onChange(deleteProperty(obj, propertyName))
    }
  }, [obj, propertyName, propertyNameError, onChange])

  return (
    <PropertyContainer>
      <Input
        value={currentPropertyName}
        error={propertyNameError}
        onChange={handlePropertyNameChange}
      />
      <div>=</div>
      <Input
        value={obj[propertyName]}
        onChange={handleValueChange}
      />
      <DeleteButton onClick={handleDelete} color="neutral"><i className="fi fi-trash"></i></DeleteButton>
    </PropertyContainer>
  )
}

const EditorContainer = styled.div`
  display: flex;
  flex-direction: column;
  gap: 1rem;
`

const FormPropertiesEditor = ({value: obj, onChange}) => {
  return (
    <EditorContainer>
      {Object.keys(obj).map((propertyName, index) => (
        <PropertyEditor key={index} obj={obj} propertyName={propertyName} onChange={onChange} />
      ))}
    </EditorContainer>
  )
}

export default FormPropertiesEditor
