import React, {forwardRef, useContext} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import FormGroupContext from './FormGroupContext.js'

const color = (props) => props.theme.color[props.color || 'error']

const textColor = (props) => {
  const _dark = props.theme.dark
  const _color = color(props)
  return _dark ? _color[300] : _color[700]
}

const FormGroupErrorComponent = styled.div`
  color: ${textColor};
  font-size: 0.875rem;
  line-height: 1.5rem;
  margin: 0.25rem 0.5rem;
`

const FormGroupError = forwardRef((props, ref) => {
  const {error} = useContext(FormGroupContext)
  return error ? (
    <FormGroupErrorComponent {...props} ref={ref}>
      {error}
    </FormGroupErrorComponent>
  ) : null
})

export default FormGroupError
