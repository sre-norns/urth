import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'

const color = (props) => props.theme.color[props.color || 'neutral']

const backgroundColor = (props) => {
  const _dark = props.theme.dark
  const _color = color(props)
  return _dark ? _color[950] : _color[100]
}

const Panel = styled.div`
  border-radius: .5rem;
  padding: 1rem;
  margin: 1rem;
  background-color: ${backgroundColor};
`

Panel.propTypes = {
}

export default Panel

