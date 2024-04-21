import React from 'react'
import PropTypes from 'prop-types'
import {Route, Switch} from 'wouter'
import styled from '@emotion/styled'
import NavRow from '../components/NavRow.js'
import NavBrand from '../components/NavBrand.js'
import NavLink from '../components/NavLink.js'
import Nav from '../components/Nav.js'
import NavRowContainer from '../components/NavRowContainer.js'
import TextInput from '../components/TextInput.js'
import Button from '../components/Button.js'
import {routed} from '../utils/routing.js'

const onNonClick = (e) => {
  e.preventDefault()
}

const SearchInput = styled(TextInput)`
  flex-grow: 1;
`

const IconButtonLink = routed(
  styled(Button)`
    //padding: 1px 5px;
    i {
      padding: 0 4px;
    }
  `.withComponent('a')
)

const Header = () => {
  // const secondLevel = true
  return (
    <>
      <Nav>
        <NavRowContainer size="large">
          <NavRow>
            <NavBrand href="/">Urth</NavBrand>
            <NavLink href="/scenarios" activePattern="/scenarios/*?">
              Scenarios
            </NavLink>
            <NavLink href="/run-results" activePattern="/run-results/*?">
              Run Results
            </NavLink>
            <NavLink href="#" onClick={onNonClick}>
              Runners
            </NavLink>
            <NavLink href="#" onClick={onNonClick} disabled active>
              Dashboards
            </NavLink>
          </NavRow>
        </NavRowContainer>
        <Switch>
          <Route path="/scenarios">
            {() => (
              <NavRowContainer size="medium">
                <NavRow center>
                  <NavLink href="#" onClick={onNonClick}>
                    Active
                  </NavLink>
                  <NavLink href="#" onClick={onNonClick}>
                    Disabled
                  </NavLink>
                  <NavLink href="#" onClick={onNonClick} active>
                    All
                  </NavLink>
                  <SearchInput placeholder="Search" />
                  <IconButtonLink href="/scenarios/new/edit" size="small" color="secondary">
                    <i className="fi fi-plus"></i>
                  </IconButtonLink>
                </NavRow>
              </NavRowContainer>
            )}
          </Route>
        </Switch>
      </Nav>
    </>
  )
}

export default Header
