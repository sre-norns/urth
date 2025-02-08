import React, { useEffect, useState } from 'react';
import PropTypes from 'prop-types'
import { Route, Switch } from 'wouter'
import { useSearchParams } from 'wouter-search';
import { useDebouncedCallback } from 'use-debounce';
import styled from '@emotion/styled'
import NavRow from '../components/NavRow.js'
import NavBrand from '../components/NavBrand.js'
import NavLink from '../components/NavLink.js'
import Nav from '../components/Nav.js'
import NavRowContainer from '../components/NavRowContainer.js'
import TextInput from '../components/TextInput.js'
import Button from '../components/Button.js'
import { routed } from '../utils/routing.js'
import { SearchQuery } from '../utils/searchQuery.js'

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


const SearchTextInput = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const [searchInput, setSearchInput] = useState(new SearchQuery(searchParams).labels);

  const debounced = useDebouncedCallback(
    (value) => {
      setSearchParams((q) => {
        console.log("User input debounced, setting query to", value)
        try {
          const query = new SearchQuery(q)
          query.labels = value
          return query.urlSearchParams
        } catch (error) {
          console.log("Failed to parse query into search query", error)
          return q;
        }
      });
    },
    600
  );

  const inputHandler = (value) => {
    debounced(value)
    setSearchInput(value)
  }

  return (<SearchInput
    placeholder="Search"
    value={searchInput}
    onChange={(e) => inputHandler(e.target.value)}
  />)
}


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
            <NavLink href="#" onClick={onNonClick}>
              Results
            </NavLink>
            <NavLink href="/runners" activePattern="/runners/*?">
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
                  <SearchTextInput placeholder="Search" />
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
