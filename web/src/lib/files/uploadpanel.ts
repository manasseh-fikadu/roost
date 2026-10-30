import type { UploadTask } from './uploads.svelte.js';

/** How long is left, as "40s left", or "" when there's no telling. */
export function eta(seconds: number): string {
	if (!Number.isFinite(seconds) || seconds <= 0) {
		return '';
	}
	if (seconds < 60) {
		return `${Math.ceil(seconds)}s left`;
	}
	if (seconds < 3600) {
		return `${Math.ceil(seconds / 60)}m left`;
	}
	return `${(seconds / 3600).toFixed(1)}h left`;
}

/** What the panel's header says about a queue. */
export function uploadSummary(tasks: Pick<UploadTask, 'status' | 'size' | 'loaded' | 'speed'>[]): {
	active: number;
	done: number;
	failed: number;
	total: number;
	loaded: number;
	speed: number;
	percent: number;
} {
	const live = tasks.filter((t) => t.status !== 'canceled');
	const total = live.reduce((n, t) => n + t.size, 0);
	const loaded = live.reduce((n, t) => n + (t.status === 'done' ? t.size : t.loaded), 0);
	return {
		active: tasks.filter((t) => t.status === 'uploading' || t.status === 'queued').length,
		done: tasks.filter((t) => t.status === 'done').length,
		failed: tasks.filter((t) => t.status === 'error').length,
		total,
		loaded,
		speed: tasks.reduce((n, t) => n + (t.status === 'uploading' ? t.speed : 0), 0),
		percent: total ? (loaded / total) * 100 : 100
	};
}
