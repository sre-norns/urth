package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/sre-norns/urth/pkg/apiserver"
	im "github.com/sre-norns/wyrd/identity/model"
	"github.com/stretchr/testify/require"
)

// identityRequest calls the API as the harness's account owner, with headers
// and a response the identity routes' contracts need: an idempotency key on a
// create, If-Match on an edit, and the ETag that answers it.
func (h *harness) identityRequest(method, path string, headers map[string]string, body any) (int, http.Header, []byte) {
	h.t.Helper()
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(h.t, err)
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(h.ctx, method, h.HTTP.URL+path, payload)
	require.NoError(h.t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(h.token))
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(h.t, err)
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	require.NoError(h.t, err)
	return res.StatusCode, res.Header, data
}

// A machine identity is a Runner's credential holder and nothing else: the two
// are created together with one UID, and the Runner's name is its queue
// address. The shared identity routes must not break that pairing.
func TestRunnerIdentityStaysPairedWithItsRunner(t *testing.T) {
	h := newHarness(t)

	code, _, data := h.identityRequest("POST", fmt.Sprintf("/v1/accounts/%s/agent-identities", h.scope.Account),
		map[string]string{"Idempotency-Key": "orphan-identity"}, map[string]string{"name": "orphan"})
	require.True(t, code >= 400 && code < 500, "a bare identity must be refused, got %d: %s", code, data)
	require.Contains(t, string(data), "/runners", "the refusal should say where runners are registered")
	var orphans int64
	require.NoError(t, h.DB.Model(&im.AgentIdentity{}).Where("name = ?", "orphan").Count(&orphans).Error)
	require.Zero(t, orphans, "the refused identity must not have been written")

	runner := h.applyRunner("paired-runner", nil)
	path := "/v1/agent-identities/" + string(runner.UID)
	code, headers, data := h.identityRequest("GET", path, nil, nil)
	require.Equal(t, http.StatusOK, code, "registering a runner still creates its identity: %s", data)

	code, _, data = h.identityRequest("PATCH", path, map[string]string{"If-Match": headers.Get("ETag")}, map[string]string{"name": "renamed"})
	require.True(t, code >= 400 && code < 500, "a rename away from the runner must be refused, got %d: %s", code, data)
	var machine im.AgentIdentity
	require.NoError(t, h.DB.Where("id = ?", runner.UID).First(&machine).Error)
	require.Equal(t, "paired-runner", machine.Name)

	code, _, data = h.identityRequest("PATCH", path, map[string]string{"If-Match": headers.Get("ETag")}, map[string]string{"status": "suspended"})
	require.Equal(t, http.StatusOK, code, "suspending a runner's identity stays allowed: %s", data)
}

// identityFlags parses identity settings exactly as cmd/api-server does.
func identityFlags(t *testing.T, args ...string) apiserver.Config {
	t.Helper()
	var cfg apiserver.Config
	parser, err := kong.New(&cfg)
	require.NoError(t, err)
	_, err = parser.Parse(args)
	require.NoError(t, err)
	return cfg
}

// With a mail provider configured, an invitation is delivered by email, and its
// link starts at the configured issuer -- the browser-facing origin -- rather
// than wherever the api-server listens. Without one (every other scenario) the
// email mode is refused and no mail worker runs.
func TestEmailInvitationIsDeliveredWithTheIssuerLink(t *testing.T) {
	mail := filepath.Join(t.TempDir(), "mail")
	flags := identityFlags(t,
		"--identity.issuer=http://localhost:3001",
		"--identity.development",
		"--identity.mail-provider=development",
		"--identity.mail-directory="+mail,
	)
	h := newHarness(t, withConfig(func(c *apiserver.Config) {
		c.IdentityOptions = flags.IdentityOptions
		c.WebRedirectURIs = []string{"http://localhost:3001/oauth/callback"}
	}))
	require.Subset(t, h.Server.Loops.Names(), []string{"identity-invitation-mail", "identity-project-access-mail"})

	code, _, data := h.identityRequest("POST", fmt.Sprintf("/v1/accounts/%s/invitations", h.scope.Account),
		map[string]string{"Idempotency-Key": "invite-new-user"},
		map[string]string{"email": "new@example.test", "role": "member", "delivery": "email"})
	require.Equal(t, http.StatusCreated, code, string(data))

	// One pass of the worker the loop runs, rather than waiting on its ticker.
	for {
		worked, err := h.Server.Identity.ProcessInvitationMail(t.Context())
		require.NoError(t, err)
		if !worked {
			break
		}
	}
	files, err := filepath.Glob(filepath.Join(mail, "*.eml"))
	require.NoError(t, err)
	require.Len(t, files, 1, "one invitation, one message")
	message, err := os.ReadFile(files[0])
	require.NoError(t, err)
	require.Contains(t, string(message), "new@example.test")

	link := regexp.MustCompile(`https?://[^\s"<>]+`).FindString(string(message))
	require.True(t, strings.HasPrefix(link, "http://localhost:3001/oauth/invitations/start?"), "the link must start at the issuer, got %q", link)

	// The issuer is the SPA's origin, which proxies /oauth/invitations/ to the
	// api-server; asked directly, the api-server accepts the same link and hands
	// the browser to the SPA's continuation route, which it does not serve.
	parsed, err := url.Parse(link)
	require.NoError(t, err)
	noRedirects := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noRedirects.Get(h.HTTP.URL + parsed.RequestURI())
	require.NoError(t, err)
	res.Body.Close()
	require.Equal(t, http.StatusSeeOther, res.StatusCode, "the emailed link must be accepted")
	require.Equal(t, "/invitations/continue", res.Header.Get("Location"))
}

func TestWithoutMailTheEmailInvitationIsRefused(t *testing.T) {
	h := newHarness(t)
	require.NotContains(t, h.Server.Loops.Names(), "identity-invitation-mail")
	code, _, _ := h.identityRequest("POST", fmt.Sprintf("/v1/accounts/%s/invitations", h.scope.Account),
		map[string]string{"Idempotency-Key": "invite-no-mail"},
		map[string]string{"email": "new@example.test", "role": "member", "delivery": "email"})
	require.Equal(t, http.StatusServiceUnavailable, code, "email delivery needs a mail provider")
}

// The bootstrap user owns an account, so it can sign straight into an
// account-only UI; bootstrap runs on every start and changes nothing the
// second time.
func TestBootstrapProvisionsAnAccountOwner(t *testing.T) {
	bootstrap := apiserver.BootstrapConfig{Email: "admin@urth.example", Password: "urth-dev-password"}
	h := newHarness(t, withConfig(func(c *apiserver.Config) { c.Bootstrap = bootstrap }))

	owned := func() (users, owners, entitlements int64) {
		var user im.User
		require.NoError(t, h.DB.Where("email = ?", bootstrap.Email).First(&user).Error)
		require.NoError(t, h.DB.Model(&im.User{}).Where("email = ?", bootstrap.Email).Count(&users).Error)
		require.NoError(t, h.DB.Model(&im.AccountMembership{}).Where("user_id = ? AND role = ?", user.ID, "owner").Count(&owners).Error)
		require.NoError(t, h.DB.Model(&im.SystemEntitlement{}).Where("user_id = ?", user.ID).Count(&entitlements).Error)
		return
	}
	users, owners, entitlements := owned()
	require.Equal(t, [3]int64{1, 1, 0}, [3]int64{users, owners, entitlements}, "one user, owning one account, with no system authority")

	// What the next start does. (A second apiserver.New cannot stand in for it:
	// identity extensions register once per database handle, and a restart is a
	// new process with a new handle.)
	require.NoError(t, h.Server.Identity.ProvisionUser(t.Context(), bootstrap.Email, bootstrap.Password, bootstrap.SystemAdmin))
	users, owners, entitlements = owned()
	require.Equal(t, [3]int64{1, 1, 0}, [3]int64{users, owners, entitlements}, "a second start changes nothing")
}

func TestBootstrapSystemAdministratorHasNoAccount(t *testing.T) {
	bootstrap := apiserver.BootstrapConfig{Email: "root@urth.example", Password: "urth-dev-password", SystemAdmin: true}
	h := newHarness(t, withConfig(func(c *apiserver.Config) { c.Bootstrap = bootstrap }))
	var user im.User
	require.NoError(t, h.DB.Where("email = ?", bootstrap.Email).First(&user).Error)
	var memberships, entitlements int64
	require.NoError(t, h.DB.Model(&im.AccountMembership{}).Where("user_id = ?", user.ID).Count(&memberships).Error)
	require.NoError(t, h.DB.Model(&im.SystemEntitlement{}).Where("user_id = ?", user.ID).Count(&entitlements).Error)
	require.Zero(t, memberships)
	require.EqualValues(t, 1, entitlements)
}

// The sign-in pages the identity module serves carry Urth's wording and
// palette, and no privacy link to a page Urth does not serve.
func TestSignInPagesAreUrths(t *testing.T) {
	h := newHarness(t)
	// The browser's way in: an authorization request from the web client.
	authorize := url.Values{
		"client_id":             {apiserver.WebClientID},
		"redirect_uri":          {"http://localhost:8080/oauth/callback"},
		"response_type":         {"code"},
		"code_challenge_method": {"S256"},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"state":                 {"state"},
	}
	res, err := http.Get(h.HTTP.URL + "/oauth/authorize?" + authorize.Encode())
	require.NoError(t, err)
	defer res.Body.Close()
	page, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode, "the authorize page did not render")
	body := string(page)
	for _, text := range []string{"Connect to your runners.", "Synthetic monitoring", "--primary-container: #22c55e;"} {
		require.True(t, strings.Contains(body, text), "the authorize page lacks %q", text)
	}
	for _, text := range []string{"Connect to your research.", "Continuous experimentation", "Privacy"} {
		require.False(t, strings.Contains(body, text), "the authorize page shows %q", text)
	}

	// The sign-in page's headline rotates through Urth's rewordings.
	res, err = http.Get(h.HTTP.URL + "/oauth/login?" + authorize.Encode())
	require.NoError(t, err)
	defer res.Body.Close()
	page, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode, "the sign-in page did not render")
	require.True(t, strings.Contains(string(page), `<h1 data-variants="[&#34;See it from inside.&#34;`),
		"the sign-in headline must carry Urth's rewordings")
}
