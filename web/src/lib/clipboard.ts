// Copying text to the clipboard, on a page that isn't always a secure context.

/**
 * Copies text to the clipboard. The clipboard API needs a secure context,
 * which Bield's HTTPS name on a Tailscale tailnet is, but a Headscale tailnet
 * has no certificates and Bield is served over plain HTTP there. Without the
 * API, this copies through a selected textarea. It throws if the browser
 * refuses.
 */
export async function copyText(text: string): Promise<void> {
	if (navigator.clipboard) {
		await navigator.clipboard.writeText(text);
		return;
	}
	const before = document.activeElement;
	// Inside a dialog if there's one, since it keeps focus within itself.
	const parent = before?.closest('[role="dialog"]') ?? document.body;
	const area = document.createElement('textarea');
	area.value = text;
	area.setAttribute('readonly', '');
	area.setAttribute('aria-hidden', 'true');
	area.style.cssText = 'position:fixed;top:0;left:0;width:1px;height:1px;opacity:0';
	parent.append(area);
	try {
		area.select();
		if (!document.execCommand('copy')) {
			throw new Error('The browser refused to copy.');
		}
	} finally {
		area.remove();
		if (before instanceof HTMLElement) {
			before.focus();
		}
	}
}
