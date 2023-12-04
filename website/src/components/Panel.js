import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'

const level = (props) => Math.max(Math.min(props.level || 0, 9), 0)

const color = (props) => props.theme.color[props.color || 'neutral']

const darkColorIndices = [950, 900, 800, 700, 600, 500, 400, 300, 200, 100]
const lightColorIndices = [100, 200, 300, 400, 500, 600, 700, 800, 900, 950]

const backgroundColor = (props) => {
  const _dark = props.theme.dark
  const _level = level(props)
  const _color = color(props)
  const _colorIndices = _dark ? darkColorIndices : lightColorIndices
  return _color[_colorIndices[_level]]
}

const Panel = styled.div`
  border-radius: .5rem;
  padding: 1rem;
  margin: 1rem;
  background-color: ${backgroundColor};
`

Panel.propTypes = {
  level: PropTypes.number,
}

export default Panel

