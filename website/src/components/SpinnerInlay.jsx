import React from 'react'
import styled from '@emotion/styled'
import Spinner from './Spinner.jsx'

const SpinnerContainer = styled.div`
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100%;
`

const SpinnerInlay = () => (
  <SpinnerContainer>
    <Spinner />
  </SpinnerContainer>
)

export default SpinnerInlay
