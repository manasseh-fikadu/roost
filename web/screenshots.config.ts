import { defineConfig } from '@playwright/test';

// Retakes the screenshots in docs/images from `roost serve --demo`. Build the
// binary first (`make build` in the repository's root), then run `make
// screenshots`. The demo listens on its default address, so the commands in the
// pictures read the way they do for a person who tries it.
const port = Number(process.env.ROOST_SCREENSHOT_PORT ?? 4700);
const url = `http://127.0.0.1:${port}`;

export default defineConfig({
	testDir: 'screenshots',
	// One browser at a time, so the charts have the same time to draw each run.
	workers: 1,
	reporter: 'list',
	use: { baseURL: url },
	projects: [
		{
			name: 'desktop',
			testMatch: 'desktop.spec.ts',
			use: { viewport: { width: 1440, height: 1000 }, colorScheme: 'dark' }
		},
		{
			name: 'phone',
			testMatch: 'phone.spec.ts',
			use: {
				viewport: { width: 390, height: 844 },
				deviceScaleFactor: 2,
				isMobile: true,
				hasTouch: true,
				colorScheme: 'dark'
			}
		}
	],
	webServer: {
		command: `../roost serve --demo --demo-listen 127.0.0.1:${port}`,
		url: `${url}/api/health`,
		timeout: 120_000,
		stderr: 'pipe'
	}
});
