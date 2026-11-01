import { test } from '@playwright/test';
import { open, out } from './shoot';

test('phone', async ({ page }) => {
	await open(page, '/alerts');
	await page.screenshot({ path: `${out}/phone.png` });
});
