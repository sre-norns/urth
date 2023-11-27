import React, {forwardRef, useContext} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import {css} from '@emotion/react'
import FormGroupContext from './FormGroupContext.js'


const errorColor = (props) => props.theme.color[props.errorColor || 'error']

const asteriskColor = (props) => errorColor(props)[props.theme.dark ? 300 : 700]

const asterisk = (props) => props.required ? css`
  &::after {
    content: '*';
    margin-left: .125rem;
    color: ${asteriskColor(props)};
  }
` : css``

const FormLabelComponent = styled.label`
  display: block;
  padding-bottom: .5rem;
  font-weight: 600;
  ${asterisk};
`

const FormLabel = forwardRef(({htmlFor, required, ...props}, ref) => {
  const {controlId} = useContext(FormGroupContext)
  return (
    <FormLabelComponent
      htmlFor={htmlFor || controlId}
      required={required}
      {...props}
      ref={ref}
    />
  )
})

FormLabel.propTypes = {
  htmlFor: PropTypes.string,
  required: PropTypes.bool,
}

export default FormLabel
