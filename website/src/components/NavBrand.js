import React from 'react'
import styled from '@emotion/styled'
import routed from '../utils/routed.js'

const color = (props) => props.theme.color[props.color || 'primary']

const textColor = (props) => {
  const _color = color(props)
  return props.theme.dark ? _color[950] : _color[50]
}

const backgroundColor = (props) => {
    const _color = color(props)
    return props.theme.dark ? _color[400] : _color[500]
}

const NavBrand = styled.a`
  font-size: 1.25rem;
  line-height: 1.5rem;
  margin-right: 1rem;
  color: ${textColor};
  background-color: ${backgroundColor};
  padding: .5rem 1rem;
  border-radius: .5rem;
`

export default routed(NavBrand)
