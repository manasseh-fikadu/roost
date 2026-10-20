import type { ErrorBody, Health } from './types.js';

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
	const res = await fetch(`/api/${path}`, { headers: { accept: 'application/json' } });
	if (!res.ok) {
		const body: Partial<ErrorBody> = await res.json().catch(() => ({}));
		throw new ApiError(res.status, body.error ?? `${res.status} ${res.statusText}`);
	}
	return res.json();
}

export function getHealth(): Promise<Health> {
	return get('health');
}
