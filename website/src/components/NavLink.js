import React from 'react'
import styled from '@emotion/styled'
import routed from '../utils/routed.js'

const color = (props) => props.theme.color[props.color || 'neutral']

const active = (props) => !!props.active && !props.disabled

const textColor = (props, state) => {
  const _dark = props.theme.dark
  const _color = color(props)
  const highlight = active(props) || (state === 'hover' && !props.disabled)
  if (_dark) {
    return _color[highlight ? 50 : 300]
  } else {
    return _color[highlight ? 950 : 500]
  }
}

const fontWeight = (props) => {
    const _active = active(props)
    return _active ? 700 : 400
}

const cursor = (props) => {
  const _disabled = !!props.disabled
  return _disabled ? 'default' : 'pointer'
}

const opacity = (props) => {
  const _disabled = !!props.disabled
  return _disabled ? 0.5 : 'unset'
}

const pointerEvents = (props) => {
  const _disabled = !!props.disabled
  return _disabled ? 'none' : 'unset'
}

const NavLink = styled.a`
  font-size: 1rem;
  font-weight: ${fontWeight};
  line-height: 1.5rem;
  cursor: ${cursor};
  color: ${textColor};
  opacity: ${opacity};
  pointer-events: ${pointerEvents};
  text-decoration: none;
  
  &:hover {
    color: ${props => textColor(props, 'hover')};
  }
`

export default routed(NavLink)
