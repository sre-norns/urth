import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'

const color = (props) => props.theme.color[props.color || 'neutral']

const backgroundColor = (props) => {
  const _color = color(props)
  return props.theme.dark ? _color[400] : _color[500]
}

const RagIndicator = styled.span`
  //position: relative;
  //top: .125em;
  display: inline-block;
  width: 0.75em;
  height: 0.75em;
  border-radius: 50%;
  background-color: ${backgroundColor};
  box-shadow: 0 0 1px 2px ${backgroundColor};
`

RagIndicator.propTypes = {
  color: PropTypes.oneOf(['primary', 'secondary', 'error', 'success', 'warning', 'neutral']),
}

export default RagIndicator
