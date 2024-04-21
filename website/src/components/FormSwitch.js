import React, {forwardRef, useCallback, useContext, useEffect, useState} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import FormGroupContext from './FormGroupContext.js'

const activeColor = (props) => props.theme.color[props.activeColor || 'success']

const inactiveColor = (props) => props.theme.color[props.inactiveColor || 'neutral']

const errorColor = (props) => props.theme.color[props.errorColor || 'error']

const focusColor = (props) => props.theme.color[props.focusColor || 'primary']

const color = (props, state) => {
  const hasError = !!props.error
  if (hasError) {
    return errorColor(props)
  }

  const active = !!props.checked
  const disabled = state === 'disabled'
  return active && !disabled ? activeColor(props) : inactiveColor(props)
}

const background = (props, state) => {
  const _color = color(props, state)
  return props.theme.dark ? _color[400] : _color[500]
}

const foregroundColor = (props, state) => {
  const _color = color(props, state)
  return props.theme.dark ? _color[100] : _color[50]
}

const focusShadowColor = (props) => {
  const hasError = !!props.error
  return (hasError && errorColor(props)) || focusColor(props)
}

const focusBoxShadow = (props) => {
  const shade = focusShadowColor(props)[props.theme.dark ? 300 : 700]
  return props.readOnly ? 'none' : `0 0 0 0.125rem ${shade}`
}

const FormSwitchComponent = styled.button`
  width: ${(props) => (props.readOnly ? '24px' : '48px')};
  height: 24px;
  background: ${background};
  border-radius: 24px;
  position: relative;
  cursor: ${(props) => (props.readOnly ? 'default' : 'pointer')};
  transition:
    background 0.3s,
    color 0.3s,
    background-color 0.3s;
  display: flex;
  justify-content: ${(props) => (props.readOnly ? 'center' : props.checked ? 'flex-start' : 'flex-end')};
  align-items: center;
  padding: 0 5px;
  color: ${foregroundColor};
  font-size: 12px;
  pointer-events: ${(props) => (props.readOnly ? 'none' : 'auto')};
  outline: none;

  &:before {
    content: ${(props) => (props.readOnly ? 'none' : "''")};
    position: absolute;
    top: 2px;
    left: ${(props) => (props.checked ? '26px' : '2px')};
    width: 20px;
    height: 20px;
    border-radius: 50%;
    background: ${foregroundColor};
    transition: left 0.3s;
  }

  &:after {
    content: ${(props) => (props.checked ? "'✓'" : "'✕'")};
    position: relative;
    padding: 0 4px;
  }

  &:focus {
    box-shadow: ${focusBoxShadow};
  }
`

const FormSwitch = forwardRef(({id, checked, onClick, ...props}, ref) => {
  const {controlId, error, validate} = useContext(FormGroupContext)

  const [prevChecked, setPrevChecked] = useState(checked)

  useEffect(() => {
    if (validate) {
      validate(checked, prevChecked, false)
    }
    setPrevChecked(checked)
  }, [checked, validate])

  const handleClick = useCallback(
    (e) => {
      e.preventDefault()
      onClick && onClick(e)
    },
    [onClick]
  )

  return (
    <FormSwitchComponent
      type="button"
      id={id || controlId}
      error={error}
      checked={checked}
      onClick={handleClick}
      {...props}
      ref={ref}
    />
  )
})

FormSwitch.propTypes = {
  id: PropTypes.string,
  checked: PropTypes.bool,
  readOnly: PropTypes.bool,
  activeColor: PropTypes.string,
  inactiveColor: PropTypes.string,
  errorColor: PropTypes.string,
  focusColor: PropTypes.string,
}

export default FormSwitch
