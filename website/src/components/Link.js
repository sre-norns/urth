import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import {routed} from '../utils/routing.js'

const Link = styled.a`
  text-decoration: none;

  &:hover {
    text-decoration: underline;
  }
`

export default routed(Link)
