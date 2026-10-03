import {expect, test} from '@playwright/test'
import {spawn, execFileSync} from 'node:child_process'
import {mkdtempSync, readFileSync, readdirSync, rmSync} from 'node:fs'
import {tmpdir} from 'node:os'
import {join} from 'node:path'

test.skip(!process.env.URTH_IDENTITY_LIVE_E2E, 'Requires isolated mail, fake IdP, API, website and CLI; see README')
test('fresh email registration, password reset, provider registration and CLI device approval', async ({page, browser}, info) => {
  test.skip(info.project.name !== 'desktop', 'One registration workflow per live stack')
  test.setTimeout(120_000)
  const mailDir = process.env.URTH_E2E_MAIL_DIR
  const cli = process.env.URTH_E2E_CLI
  const idp = process.env.URTH_E2E_IDP_URL
  const endpoint = process.env.URTH_E2E_API_URL
  if (!mailDir || !cli || !idp || !endpoint) throw new Error('Set URTH_E2E_MAIL_DIR, URTH_E2E_CLI, URTH_E2E_IDP_URL and URTH_E2E_API_URL')
  const suffix = Date.now().toString(36)
  const email = `email-${suffix}@example.test`
  const password = `first-${suffix}-password`
  const resetPassword = `reset-${suffix}-password`
  async function mailLink(recipient: string, route: string) {
    let link = ''
    await expect.poll(() => {
      for (const name of readdirSync(mailDir)) {
        const mail = readFileSync(join(mailDir, name), 'utf8')
        if (!mail.includes(recipient)) continue
        link = mail.match(new RegExp(`https?://[^\\s<>"']+${route}[^\\s<>"']*`))?.[0] ?? link
      }
      return Boolean(link)
    }, {timeout: 15_000, message: `Development mail delivers ${route}`}).toBe(true)
    return link
  }
  async function passwordLogin(secret: string) {
    await page.goto('/sign-in')
    await page.getByLabel('Email', {exact: true}).fill(email)
    await page.getByLabel('Password', {exact: true}).fill(secret)
    await page.getByRole('button', {name: 'Log in', exact: true}).click()
    await page.waitForURL(/\/a\//)
  }
  await page.goto('/oauth/register')
  await page.getByLabel('Email', {exact: true}).fill(email)
  await page.getByLabel('Account name', {exact: true}).fill(`Email ${suffix}`)
  await page.getByRole('button', {name: 'Send verification link'}).click()
  await expect(page.getByRole('heading', {name: 'Check your email'})).toBeVisible()
  const verification = await mailLink(email, '/oauth/verify-email')
  await page.goto(verification)
  await page.getByLabel('New password', {exact: true}).fill(password)
  await page.getByLabel('Confirm password', {exact: true}).fill(password)
  await page.getByRole('button', {name: 'Create account', exact: true}).click()
  await expect(page.getByRole('heading', {name: 'Account created'})).toBeVisible()
  await page.goto(verification)
  await expect(page.getByRole('heading', {name: 'Link unavailable'})).toBeVisible()
  await passwordLogin(password)
  const oldSession = await page.evaluate(() => JSON.parse(sessionStorage.getItem('urth.session')!).access_token as string)
  expect((await page.request.get('/v1/profile', {headers: {Authorization: `Bearer ${oldSession}`}})).status()).toBe(200)
  await page.goto('/oauth/forgot-password')
  await page.getByLabel('Email', {exact: true}).fill(email)
  await page.getByRole('button', {name: 'Send reset link'}).click()
  await page.goto(await mailLink(email, '/oauth/reset-password'))
  await page.getByLabel('New password', {exact: true}).fill(resetPassword)
  await page.getByLabel('Confirm password', {exact: true}).fill(resetPassword)
  await page.getByRole('button', {name: 'Update password'}).click()
  await expect(page.getByRole('heading', {name: 'Password updated'})).toBeVisible()
  const revoked = await page.request.get('/v1/profile', {headers: {Authorization: `Bearer ${oldSession}`}})
  expect(revoked.status()).toBe(401)
  await page.evaluate(() => sessionStorage.clear())
  await page.goto('/sign-in')
  await page.getByLabel('Email', {exact: true}).fill(email)
  await page.getByLabel('Password', {exact: true}).fill(password)
  await page.getByRole('button', {name: 'Log in', exact: true}).click()
  await expect(page.getByRole('alert')).toBeVisible()
  await passwordLogin(resetPassword)

  const dir = mkdtempSync(join(tmpdir(), 'urth-device-e2e-'))
  const env = {...process.env, URTH_PROFILES: join(dir, 'profiles.json')}
  const login = spawn(cli, ['auth', 'login', `--api-server-address=${endpoint}`], {env, stdio: ['ignore', 'pipe', 'pipe']})
  let output = ''
  login.stdout.on('data', (chunk: Buffer) => {output += chunk.toString()})
  login.stderr.on('data', (chunk: Buffer) => {output += chunk.toString()})
  try {
    await expect.poll(() => output.match(/https?:\/\/[^\s]+\/oauth\/device[^\s]*/)?.[0], {timeout: 10_000}).toBeTruthy()
    const device = await browser.newPage()
    await device.goto(output.match(/https?:\/\/[^\s]+\/oauth\/device[^\s]*/)![0])
    await device.getByLabel('Email', {exact: true}).fill(email)
    await device.getByLabel('Password', {exact: true}).fill(resetPassword)
    await device.getByRole('button', {name: 'Approve access'}).click()
    if (await device.locator('select[name="authority_context"]').isVisible()) {
      const authority = await device.locator('select[name="authority_context"] option').evaluateAll((options) => options.map((option) => (option as HTMLOptionElement).value).find(Boolean))
      await device.locator('select[name="authority_context"]').selectOption(authority!)
      await device.getByRole('button', {name: 'Approve access'}).click()
    }
    await expect.poll(() => login.exitCode, {timeout: 15_000}).toBe(0)
    const status = execFileSync(cli, ['auth', 'status'], {env, encoding: 'utf8'})
    expect(status).toContain(endpoint)
    expect(status).toContain('owner')
    execFileSync(cli, ['projects', 'create', `device-${suffix}`, '--use'], {env, encoding: 'utf8'})
    expect(execFileSync(cli, ['projects', 'list', '-o', 'json'], {env, encoding: 'utf8'})).toContain(`device-${suffix}`)
    await device.close()
  } finally {
    if (login.exitCode === null && login.signalCode === null) {
      login.kill('SIGTERM')
      await new Promise<void>((resolve) => login.once('exit', () => resolve()))
    }
    rmSync(dir, {recursive: true, force: true})
  }

  const providerEmail = `provider-${suffix}@example.test`
  const identity = {subject: `subject-${suffix}`, email: providerEmail, email_verified: true}
  const providerContext = await browser.newContext()
  const providerPage = await providerContext.newPage()
  try {
    expect((await page.request.post(`${idp}/control/next`, {data: {provider: 'google', identity}})).ok()).toBe(true)
    await providerPage.goto('/oauth/register')
    await providerPage.getByRole('link', {name: 'Continue with OpenID Connect'}).click()
    await providerPage.getByLabel('Account name', {exact: true}).fill(`Provider ${suffix}`)
    await providerPage.getByRole('button', {name: 'Send confirmation', exact: true}).click()
    await providerPage.goto(await mailLink(providerEmail, '/oauth/providers/'))
    await providerPage.getByRole('button', {name: /Confirm/}).click()
    await expect(providerPage.getByRole('heading', {name: 'Account created'})).toBeVisible()
    expect((await page.request.post(`${idp}/control/next`, {data: {provider: 'google', identity}})).ok()).toBe(true)
    await providerPage.goto('/sign-in')
    await providerPage.getByRole('link', {name: 'Continue with OpenID Connect'}).click()
    await providerPage.waitForURL(/\/a\//)
    await expect(providerPage.getByRole('heading', {name: 'Projects', exact: true})).toBeVisible()
  } finally {
    await providerContext.close()
  }
})
