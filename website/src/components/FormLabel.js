import React, {forwardRef, useContext} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import {css} from '@emotion/react'
import FormGroupContext from './FormGroupContext.js'
import Label from './Label.js'

const FormLabel = forwardRef(({htmlFor, required, ...props}, ref) => {
  const {controlId} = useContext(FormGroupContext)
  return <Label htmlFor={htmlFor || controlId} required={required} {...props} ref={ref} />
})

FormLabel.propTypes = {
  htmlFor: PropTypes.string,
  required: PropTypes.bool,
}

export default FormLabel
