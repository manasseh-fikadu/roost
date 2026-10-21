import type { ErrorBody, Health, Me } from './types.js';

/**
 * Roost refuses requests that change things unless they carry this header,
 * which a page on another site can't add. It matches CSRFHeader in
 * internal/api/auth.go.
 */
const csrfHeaders = { 'x-roost': '1' };

/** An error response from the Roost API. */
export class ApiError extends Error {
	readonly status: number;

	constructor(status: number, message: string) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
	}
}

/** The global fetch, or the one SvelteKit passes to a `load` function. */
type Fetch = typeof fetch;

async function get<T>(path: string, fetcher: Fetch = fetch): Promise<T> {
	const res = await fetcher(`/api/${path}`, {
		headers: { accept: 'application/json', ...csrfHeaders }
	});
	if (!res.ok) {
		const body: Partial<ErrorBody> = await res.json().catch(() => ({}));
		throw new ApiError(res.status, body.error ?? `${res.status} ${res.statusText}`);
	}
	return res.json();
}

export function getHealth(): Promise<Health> {
	return get('health');
}

/**
 * Who Roost thinks you are, from the device you're on. From a `load`
 * function, pass the `fetch` it's given.
 */
export function getMe(fetcher?: Fetch): Promise<Me> {
	return get('me', fetcher);
}
