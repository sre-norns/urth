package prob

type PuppeteerOptions struct {
	Headless        bool
	PageWaitSeconds int

	WorkingDirectory string
	KeepTempDir      bool
	TempDirPrefix    string
}

type HttpOptions struct {
	CaptureResponseBody bool
	CaptureRequestBody  bool
	IgnoreRedirects     bool
}

type HarOptions struct {
	CompareWithOriginal bool
}

type RunOptions struct {
	Puppeteer PuppeteerOptions
	Http      HttpOptions
	Har       HarOptions
}
