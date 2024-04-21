import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'

const NavRow = styled.div`
  display: flex;
  flex-direction: row;
  align-items: ${(props) => (props.center ? 'center' : 'baseline')};
  //justify-content: center;
  padding: 0 1.5rem;
  gap: 1rem;
`

NavRow.defaultProps = {
  center: PropTypes.bool,
}

export default NavRow
