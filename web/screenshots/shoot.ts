import type { Page } from '@playwright/test';

/** Where the pictures go, from web/, which is where `make screenshots` runs. */
export const out = '../docs/images';

/** Opens a page of the demo, and waits for the live feed to fill it in. */
export async function open(page: Page, path: string) {
	// The first-run setup would take over the overview.
	await page.addInitScript(() => sessionStorage.setItem('roost:setup-sent', '1'));
	await page.goto(path);
	await page.getByRole('note').filter({ hasText: 'made-up machines' }).waitFor();
	await settle(page);
}

/** Gives the charts and the live numbers a moment to draw. */
export async function settle(page: Page) {
	await page.waitForTimeout(3000);
}

/** The link to a device's or container's page, where it's shown (a phone has a list and a table). */
export function link(page: Page, href: string, text: string) {
	return page
		.locator(`a[href^="${href}"]`)
		.filter({ hasText: text })
		.locator('visible=true')
		.first();
}
