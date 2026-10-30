import { expect, test } from 'bun:test';
import type { FileEntry } from '#lib/api/types.js';
import { bulkMessage, filesUrl, isHidden, terminalHref, totals } from './browser.js';

test('filesUrl', () => {
	expect(filesUrl('laptop', null)).toBe('/files?device=laptop');
	expect(filesUrl('laptop', '/a b')).toBe('/files?device=laptop&path=%2Fa+b');
	expect(filesUrl('n1', '/a', '/a/b?c#d')).toBe('/files?device=n1&path=%2Fa&file=%2Fa%2Fb%3Fc%23d');
});

test('terminalHref escapes the folder', () => {
	expect(terminalHref('/terminal', 'laptop', '/a b&c')).toBe(
		'/terminal?device=laptop&cwd=%2Fa%20b%26c'
	);
});

test('bulkMessage', () => {
	expect(bulkMessage('Moved', { done: 3, failed: [] })).toEqual({
		text: 'Moved 3 items',
		error: false
	});
	expect(bulkMessage('Moved', { done: 1, failed: [] }).text).toBe('Moved 1 item');
	expect(bulkMessage('Deleted', { done: 2, failed: [{ path: '/x', error: 'no' }] })).toEqual({
		text: 'Deleted 2, 1 failed: no',
		error: true
	});
});

const e = (name: string, extra: Partial<FileEntry> = {}): FileEntry => ({
	name,
	path: `/${name}`,
	type: 'file',
	size: 10,
	modifiedAt: '',
	mode: '',
	owner: '',
	group: '',
	...extra
});

test('hidden and totals', () => {
	expect(isHidden(e('.git'))).toBe(true);
	expect(isHidden(e('a.git'))).toBe(false);
	expect(
		totals([
			e('a'),
			e('b', { size: 5 }),
			e('d', { type: 'dir', size: 4096 }),
			e('l', { type: 'symlink', targetType: 'dir' })
		])
	).toEqual({ folders: 2, files: 2, size: 15 });
});
