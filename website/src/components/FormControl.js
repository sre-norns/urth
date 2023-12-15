import React, {forwardRef, useCallback, useContext, useEffect} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import FormGroupContext from './FormGroupContext.js'
import Input from './Input.js'


const FormControl = forwardRef(({id, value, onBlur, ...props}, ref) => {
  const {controlId, error, validate} = useContext(FormGroupContext)

  const [prevValue, setPrevValue] = React.useState(value)

  useEffect(() => {
    if (validate) {
      validate(value, prevValue, false)
    }
    setPrevValue(value)
  }, [value, validate])

  const handleBlur = useCallback((e) => {
    onBlur && onBlur(e)
    validate && validate(value, prevValue, true)
  }, [onBlur, validate, value, prevValue])

  return (
    <Input
      id={id || controlId}
      value={value}
      error={error}
      onBlur={handleBlur}
      {...props}
      ref={ref}
    />
  )
})

export default FormControl
