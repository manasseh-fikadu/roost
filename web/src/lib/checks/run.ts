import { runCheck } from '#lib/api/client.js';
import { toast } from '#lib/toast.svelte.js';

/**
 * Runs a check now, and says in a toast how it went. If the request itself
 * fails, the client says why and this throws.
 */
export async function runCheckNow(check: { id: string; name: string }): Promise<void> {
	const res = await runCheck(check.id);
	toast(
		`${check.name} ${res.ok ? 'passed' : 'failed'}: ${res.detail}`,
		res.ok ? 'default' : 'error'
	);
}
