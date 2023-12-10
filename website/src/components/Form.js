import React, {Component} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import FormContext from './FormContext.js'
import {isEmpty} from '../utils/objects.js'


const FormContainer = styled.form``

class Form extends Component {
  constructor(props) {
    super(props)

    this.formContext = {
      register: this.onRegister,
      unregister: this.onUnregister,
      validated: this.onValidated,
    }

    this.validators = {}
    this.errors = {}

    this.initialValues = {}
    this.changedValues = {}
  }

  get isValid() {
    return Object.keys(this.errors).length === 0
  }

  validate() {
    for (const validate of Object.values(this.validators)) {
      validate()
    }

    return this.isValid
  }

  get hasChanges() {
    return !isEmpty(this.changedValues)
  }

  resetChangeTracking() {
    this.initialValues = {...this.initialValues, ...this.changedValues}
    this.changedValues = {}
  }

  onRegister = (controlId, validate) => {
    if (this.validators[controlId]) {
      console.warn('Duplicate controlId', controlId)
    }

    this.validators[controlId] = validate
  }

  onUnregister = (controlId) => {
    if (!this.validators[controlId]) {
      console.warn('Missing controlId', controlId)
    }

    delete this.validators[controlId]
  }

  onValidated = (controlId, value, error) => {
    if (this.validators[controlId] === undefined) {
      console.warn('Validation result from unknown controlId', controlId)
    }

    if (error) {
      this.errors[controlId] = error
    } else {
      delete this.errors[controlId]
    }

    if (this.initialValues.hasOwnProperty(controlId)) {
      if (this.initialValues[controlId] === value) {
        delete this.changedValues[controlId]
      } else {
        this.changedValues[controlId] = value
      }
    } else {
      this.initialValues[controlId] = value
    }

    if (this.props.onValidated) {
      this.props.onValidated(this.isValid, this.hasChanges)
    }
  }

  render() {
    const {onValidated, children, ...props} = this.props

    return (
      <FormContext.Provider value={this.formContext}>
        <FormContainer {...props}>{children}</FormContainer>
      </FormContext.Provider>
    )
  }
}

Form.propTypes = {
  onValidated: PropTypes.func,
}

export default Form
