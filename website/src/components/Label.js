import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import {css} from '@emotion/react'


const errorColor = (props) => props.theme.color[props.errorColor || 'error']

const asteriskColor = (props) => errorColor(props)[props.theme.dark ? 300 : 700]

const asterisk = (props) => props.required ? css`
  &::after {
    content: '*';
    margin-left: .125rem;
    color: ${asteriskColor(props)};
  }
` : css``

const Label = styled.label`
  display: block;
  padding-bottom: .5rem;
  font-weight: 600;
  ${asterisk};
`

Label.propTypes = {
  htmlFor: PropTypes.string,
  required: PropTypes.bool,
}

export default Label
