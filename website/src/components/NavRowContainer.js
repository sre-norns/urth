import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'

const minHeight = (props) => {
  switch (props.size) {
    case 'small': return '32px'
    case 'medium': return '48px'
    case 'large': return '64px'
    default: return 'unset'
  }
}

const NavRowContainer = styled.div`
  display: flex;
  flex-direction: column;
  justify-content: center;

  min-height: ${minHeight};
`

NavRowContainer.propTypes = {
  size: PropTypes.oneOf(['small', 'medium', 'large']),
}

export default NavRowContainer
