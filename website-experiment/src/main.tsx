import {StrictMode} from 'react'
import {createRoot} from 'react-dom/client'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {createBrowserRouter, RouterProvider} from 'react-router-dom'
import {IdentityProvider} from '@sre-norns/components/identity'
import {routes} from './App'
import {scopedLinks} from './identity/links'
import {identityTerms, runnerRegistration} from './identity/config'
import {session} from './identity/session'
import '@fontsource-variable/hanken-grotesk'
import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/jetbrains-mono/500.css'
import '@sre-norns/components/styles.css'
import '@sre-norns/components/themes/urth.css'
import './monitoring/monitoring.css'

const links = scopedLinks(session.accountId)
const router = createBrowserRouter(routes)

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      refetchOnWindowFocus: true,
      retry: 1,
    },
    mutations: {retry: 0},
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <IdentityProvider
        session={session}
        queryClient={queryClient}
        terms={identityTerms}
        links={links}
        machineRegistration={runnerRegistration}
      >
        <RouterProvider router={router} />
      </IdentityProvider>
    </QueryClientProvider>
  </StrictMode>,
)
