import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'

const baseColor = (props) => props.theme.color[props.baseColor || 'neutral']

const errorColor = (props) => props.theme.color[props.errorColor || 'error']

const focusColor = (props) => props.theme.color[props.focusColor || 'primary']

const hoverColor = (props) => props.theme.color[props.hoverColor || 'contrast']

const textColor = (props, state) => {
  const _baseColor = baseColor(props)
  switch (state) {
    case 'placeholder':
      return _baseColor[props.theme.dark ? 300 : 700]
    default:
      return _baseColor[props.theme.dark ? 50 : 950]
  }
}

const backgroundColor = (props, state) => {
  const _baseColor = baseColor(props)
  const _dark = props.theme.dark
  const disabled = state === 'disabled'
  return !disabled ? _baseColor[_dark ? 950 : 50] : _baseColor[_dark ? 700 : 300]
}

const borderColor = (props, state) => {
  const hasError = !!props.error
  const highlight = state === 'focus' || state === 'hover'
  return (
    (hasError && errorColor(props)) ||
    (state === 'focus' && focusColor(props)) ||
    (state === 'hover' && hoverColor(props)) ||
    baseColor(props)
  )
}

const border = (props, state) => {
  const shade = borderColor(props, state)[props.theme.dark ? 300 : 700]
  return `1px solid ${shade}`
}

const focusShadowColor = (props) => {
  const hasError = !!props.error
  return (hasError && errorColor(props)) || focusColor(props)
}

const focusBoxShadow = (props) => {
  const shade = focusShadowColor(props)[props.theme.dark ? 300 : 700]
  return `0 0 0 0.125rem ${shade}`
}

const Input = styled.input`
  display: block;
  width: 100%;
  padding: 0.25rem 0.5rem;
  font-size: 1rem;
  font-weight: 400;
  line-height: 1.5rem;
  color: ${textColor};
  background-color: ${backgroundColor};
  border: ${border};
  border-radius: 0.5rem;
  transition:
    border-color 0.125s ease-in-out,
    box-shadow 0.125s ease-in-out;

  ::placeholder {
    color: ${(props) => textColor(props, 'placeholder')};
  }

  &:hover {
    border: ${(props) => border(props, 'hover')};
  }

  &:focus {
    border: ${(props) => border(props, 'focus')};
    box-shadow: ${focusBoxShadow};
  }
`

Input.propTypes = {
  baseColor: PropTypes.string,
  errorColor: PropTypes.string,
  focusColor: PropTypes.string,
  hoverColor: PropTypes.string,
}

export default Input
