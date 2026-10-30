import type { FileEntry } from '#lib/api/types.js';
import { isDirLike } from './kinds.js';

export type SortKey = 'name' | 'size' | 'mtime' | 'mode' | 'owner';

export interface Sort {
	key: SortKey;
	/** 1 for ascending, -1 for descending. */
	dir: 1 | -1;
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

/** Folders first, then by the column; names compare naturally, so "file2" comes before "file10". */
export function sortEntries(list: FileEntry[], sort: Sort): FileEntry[] {
	return [...list].sort((a, b) => {
		const da = isDirLike(a);
		const db = isDirLike(b);
		if (da !== db) {
			return da ? -1 : 1;
		}
		let c = 0;
		if (sort.key === 'size') {
			c = (da ? 0 : a.size) - (db ? 0 : b.size);
		} else if (sort.key === 'mtime') {
			c = Date.parse(a.modifiedAt) - Date.parse(b.modifiedAt);
		} else if (sort.key === 'mode') {
			c = a.mode.localeCompare(b.mode);
		} else if (sort.key === 'owner') {
			c = collator.compare(a.owner, b.owner);
		}
		return (c || collator.compare(a.name, b.name)) * sort.dir;
	});
}

/** What clicking a column header does: reverse the sort if it's the column, or else sort by it. */
export function nextSort(sort: Sort, key: SortKey): Sort {
	if (sort.key === key) {
		return { key, dir: sort.dir === 1 ? -1 : 1 };
	}
	// Names go A to Z, and the rest biggest or latest first.
	return { key, dir: key === 'name' ? 1 : -1 };
}
