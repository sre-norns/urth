import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import {DATA_CLASS_DESCRIPTIONS, DataClass} from '../utils/labels.js'

const colorFor = (dataClass) => {
  switch (dataClass) {
    case DataClass.Clean:
      return 'success'
    case DataClass.Redacted:
      return 'primary'
    case DataClass.SecretBearing:
      return 'error'
    default:
      return 'warning'
  }
}

const Badge = styled.span`
  display: inline-block;
  padding: 0 0.5rem;
  border-radius: 0.75rem;
  font-size: 0.75rem;
  line-height: 1.25rem;
  white-space: nowrap;
  color: ${(props) => props.theme.color[colorFor(props.dataClass)][props.theme.dark ? 200 : 900]};
  background-color: ${(props) => props.theme.color[colorFor(props.dataClass)][props.theme.dark ? 900 : 100]};
`

// Surfaces what an artifact may expose, so that a HAR recording carrying live
// credentials is visibly different from a redacted log before anyone opens it.
const DataClassBadge = ({dataClass, ...rest}) => (
  <Badge
    dataClass={dataClass}
    title={DATA_CLASS_DESCRIPTIONS[dataClass] || DATA_CLASS_DESCRIPTIONS[DataClass.Unknown]}
    {...rest}
  >
    {dataClass}
  </Badge>
)

DataClassBadge.propTypes = {
  dataClass: PropTypes.string.isRequired,
}

export default DataClassBadge
