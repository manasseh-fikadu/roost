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

async function get<T>(path: string): Promise<T> {
	const res = await fetch(`/api/${path}`, {
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

/** Who Roost thinks you are, from the device you're on. */
export function getMe(): Promise<Me> {
	return get('me');
}
