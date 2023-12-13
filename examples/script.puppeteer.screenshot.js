const puppeteer = require('puppeteer');
const PuppeteerHar = require('puppeteer-har');


(async () => {
  const browser = await puppeteer.launch({
    headless: ("URTH_PUPPETEER_HEADLESS" in process.env) ? (process.env.URTH_PUPPETEER_HEADLESS === 'true') : "new",
    slowMo: process.env.URTH_PUPPETEER_PAGE_WAIT
  });
  const page = await browser.newPage();
  const har = new PuppeteerHar(page);
  await har.start({ path: 'results.har' });

  await page.goto('http://localhost:8180/scenarios');
  await page.screenshot({path: 'selfie.png'});

  await har.stop();
  await browser.close();
})();