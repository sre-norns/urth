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

// Note: this was previously assigned to defaultProps, which set `center` to the
// PropTypes validator itself -- a truthy value -- so every NavRow rendered
// centred and the prop had no effect. React 19 drops defaultProps on function
// components, which is what surfaced it.
NavRow.propTypes = {
  center: PropTypes.bool,
}

export default NavRow
