import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'

const color = (props) => props.theme.color[props.color || 'neutral']

const borderRadius = (props) => (!!props.odd ? '.5rem' : '0')

const backgroundColor = (props) => {
  const _dark = props.theme.dark
  const _color = color(props)
  const _odd = !!props.odd
  return _odd ? (_dark ? _color[950] : _color[100]) : 'transparent'
}

const OddContainer = styled.div`
  border-radius: ${borderRadius};
  padding: 1rem;
  background-color: ${backgroundColor};
`

OddContainer.propTypes = {
  odd: PropTypes.bool,
}

export default OddContainer
