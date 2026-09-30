import {createSessionClient, type SessionConfig} from '@sre-norns/components/identity'

/**
 * Urth's OAuth client. Stored sessions and refresh tokens are bound to the
 * client ID and the storage keys, so neither may change.
 */
export const sessionConfig: SessionConfig = {
  clientId: 'urth-web',
  storageKeys: {
    session: 'urth.session',
    authorization: 'urth.authorization',
    accountArchived: 'urth.account-archived',
  },
}

/** The web app's one session: the shared identity client bound to Urth's. */
export const session = createSessionClient(sessionConfig)
