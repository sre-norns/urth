import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'

const baseColor = (props) => props.theme.color[props.baseColor || 'neutral']

const accentColor = (props) => props.theme.color[props.accentColor || 'secondary']

const textColor = (props) => {
  const _baseColor = baseColor(props)
  return _baseColor[props.theme.dark ? 300 : 700]
}

const backgroundColor = (props, state) => {
  const _baseColor = baseColor(props)
  const _dark = props.theme.dark
  const disabled = state === 'disabled'
  return !disabled ? _baseColor[_dark ? 950 : 50] : _baseColor[_dark ? 700 : 300]
}

const borderColor = (props, state) => {
  const highlight = state === 'focus' || state === 'hover'
  return highlight ? accentColor(props) : baseColor(props)
}

const border = (props) => {
  const shade = borderColor(props)[props.theme.dark ? 300 : 700]
  return `1px solid ${shade}`
}

const TextInput = styled.input`
  display: block;
  width: 100%;
  padding: 0.125rem 0.5rem;
  font-size: 1rem;
  font-weight: 400;
  line-height: 1.5rem;
  color: ${textColor};
  background-color: ${backgroundColor};
  border: ${border};
  border-radius: 0.5rem;
  
  ::placeholder {
    color: ${textColor};
  }
`

TextInput.defaultProps = {
  type: 'text',
}

TextInput.propTypes = {
  baseColor: PropTypes.string,
}

export default TextInput
