import React, { useCallback } from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import Input from './Input.js'
import Label from './Label.js'

const PropertyEditor = ({obj, propertyName, onChange}) => {
  const handleChange = useCallback((e) => {
    onChange && onChange({...obj, [propertyName]: e.target.value})
  }, [obj, propertyName, onChange])

  return (
    <div>
      <Label htmlFor={propertyName}>{propertyName}</Label>
      <Input
        id={propertyName}
        name={propertyName}
        value={obj[propertyName]}
        onChange={handleChange}
      />
    </div>
  )
}

const FormPropertiesEditor = ({value: obj, onChange}) => {
  // const handleChange = (propertyName, propertyValue) => {
  //   onChange && onChange({...obj, [propertyName]: propertyValue})
  // }

  return (
    <>
      {Object.entries(obj).map(([propertyName, propertyValue]) => (
        <PropertyEditor key={propertyName} obj={obj} propertyName={propertyName} onChange={onChange} />
      ))}
    </>
  )
}

export default FormPropertiesEditor
