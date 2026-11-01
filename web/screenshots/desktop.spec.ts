import { test } from '@playwright/test';
import { link, open, out, settle } from './shoot';

// The pictures in README.md and docs/. A page that changes in a way that
// matters for them shows up as a different file in git.

for (const [name, path] of [
	['overview', '/'],
	['alerts', '/alerts'],
	['checks', '/checks'],
	['docker', '/docker'],
	['notifications', '/settings?tab=notifications'],
	['agents', '/devices/agents']
] as const) {
	test(name, async ({ page }) => {
		await open(page, path);
		await page.screenshot({ path: `${out}/${name}.png` });
	});
}

test('device', async ({ page }) => {
	await open(page, '/devices');
	await link(page, '/devices/', 'NAS').click();
	await page.waitForURL(/\/devices\/[^/]+$/);
	await settle(page);
	await page.screenshot({ path: `${out}/device.png` });
});

test('container', async ({ page }) => {
	await open(page, '/docker');
	await link(page, '/docker/', 'caddy').click();
	await page.waitForURL(/\/docker\/caddy/);
	await settle(page);
	await page.screenshot({ path: `${out}/container.png` });
});
