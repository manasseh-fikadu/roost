// The file manager's URLs and small decisions, apart from the page, so they can be tested.

import type { FileEntry } from '#lib/api/types.js';
import { plural } from '#lib/format.js';
import { isDirLike } from './kinds.js';

/** The file manager's address, for a device's folder and optionally a file open over it. */
export function filesUrl(device: string, path: string | null, file?: string | null): string {
	const q = new URLSearchParams({ device });
	if (path) {
		q.set('path', path);
	}
	if (file) {
		q.set('file', file);
	}
	return `/files?${q.toString()}`;
}

/** The terminal page, in a folder of a device. */
export function terminalHref(terminalPath: string, device: string, dir: string): string {
	return `${terminalPath}?device=${encodeURIComponent(device)}&cwd=${encodeURIComponent(dir)}`;
}

/** What to tell someone after an operation on several paths. */
export function bulkMessage(
	verb: string,
	r: { done: number; failed: Array<{ path: string; error: string }> }
): { text: string; error: boolean } {
	if (r.failed.length) {
		return {
			text: `${verb} ${r.done}, ${r.failed.length} failed: ${r.failed[0].error}`,
			error: true
		};
	}
	return { text: `${verb} ${plural(r.done, 'item')}`, error: false };
}

/** Whether a name is hidden, which is a dot at its start. */
export const isHidden = (e: Pick<FileEntry, 'name'>): boolean => e.name.startsWith('.');

/** The numbers under the listing: folders, files and the files' bytes. */
export function totals(list: FileEntry[]): { folders: number; files: number; size: number } {
	const folders = list.filter(isDirLike).length;
	return {
		folders,
		files: list.length - folders,
		size: list.reduce((n, e) => n + (isDirLike(e) ? 0 : e.size), 0)
	};
}
