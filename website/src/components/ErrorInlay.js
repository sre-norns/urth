import React from "react";
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import Error from "./Error.js"

const Container = styled.div`
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100%;
  text-align: center;
`

const ErrorInlay = ({message, details}) => (
  <Container>
    <Error message={message} details={details} />
  </Container>
)


Error.propTypes = {
    message: PropTypes.string.isRequired,
    details: PropTypes.string
}
export default ErrorInlay