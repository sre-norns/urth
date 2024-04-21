import React from 'react'
import PropTypes from 'prop-types'

const Error = ({message, details}) => (
  <div>
    <h1>{message}</h1>
    <h2>{details}</h2>
  </div>
)

Error.propTypes = {
  message: PropTypes.string.isRequired,
  details: PropTypes.string,
}

export default Error
