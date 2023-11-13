import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import Color from 'color'

const color = (props) => props.theme.color[props.color || 'neutral']

const backgroundColor = (props) => {
  const _color = color(props)
  const shade = _color[props.theme.dark ? 950 : 50]
  return Color(shade).alpha(0.75).string()
}

const borderBottom = (props) => {
  const _color = color(props)
  const shade = _color[props.theme.dark ? 900 : 200]
  const tuned = Color(shade).alpha(0.5).string()
  return `1px solid ${tuned}`
}

const Nav = styled.nav`
  position: sticky;
  top: 0;
  z-index: 1;

  background-color: ${backgroundColor};
  backdrop-filter: blur(16px);
  border-bottom: ${borderBottom};
`

Nav.propTypes = {
  children: PropTypes.node,
}

export default Nav