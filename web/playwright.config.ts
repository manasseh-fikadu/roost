import { defineConfig, devices } from '@playwright/test';

// The smoke test runs against `roost serve --demo`, which needs no Tailscale.
// Build the binary first (`make build` in the repository's root), since it
// carries the web app.
const port = Number(process.env.ROOST_E2E_PORT ?? 4799);
const url = `http://127.0.0.1:${port}`;

export default defineConfig({
	testDir: 'e2e',
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 1 : 0,
	reporter: process.env.CI ? [['list'], ['github']] : 'list',
	use: { baseURL: url, trace: 'retain-on-failure' },
	projects: [
		{ name: 'desktop', use: { ...devices['Desktop Chrome'] } },
		{ name: 'phone', use: { ...devices['Pixel 7'] } }
	],
	webServer: {
		command: `../roost serve --demo --demo-days 1 --demo-listen 127.0.0.1:${port}`,
		url: `${url}/api/health`,
		// A server that's already there is the one `make dev` or the benchmark runs.
		reuseExistingServer: !process.env.CI,
		timeout: 120_000,
		stderr: 'pipe'
	}
});
