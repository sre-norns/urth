import React, {forwardRef, useContext} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import FormGroupContext from './FormGroupContext.js'


const FormLabelComponent = styled.label`
  display: block;
  padding-bottom: .5rem;
  font-weight: 600;
`

const FormLabel = forwardRef(({htmlFor, ...props}, ref) => {
  const {controlId} = useContext(FormGroupContext)
  return (
    <FormLabelComponent
      htmlFor={htmlFor || controlId}
      {...props}
      ref={ref}
    />
  )
})

FormLabel.propTypes = {
  htmlFor: PropTypes.string,
}

export default FormLabel
