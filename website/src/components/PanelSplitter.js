import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'

const color = (props) => props.theme.color[props.color || 'neutral']

const borderColor = (props) => {
  const _dark = props.theme.dark
  const _color = color(props)
  return _dark ? _color[500] : _color[600]
}

const level = (props) => (props.level === undefined ? 1 : props.level)

const PanelSplitter = styled.div`
  margin: ${level}rem 0;
  border-top: 1px solid ${borderColor};
`

PanelSplitter.propTypes = {
  level: PropTypes.number,
}

export default PanelSplitter
