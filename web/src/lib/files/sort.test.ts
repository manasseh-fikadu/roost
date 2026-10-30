import { expect, test } from 'bun:test';
import type { FileEntry } from '#lib/api/types.js';
import { nextSort, sortEntries } from './sort.js';

const entry = (name: string, extra: Partial<FileEntry> = {}): FileEntry => ({
	name,
	path: `/${name}`,
	type: 'file',
	size: 0,
	modifiedAt: '2026-01-01T00:00:00Z',
	mode: '-rw-r--r--',
	owner: 'me',
	group: 'me',
	...extra
});

const names = (list: FileEntry[]) => list.map((e) => e.name);

test('folders come first, and names compare naturally', () => {
	const list = [
		entry('file10'),
		entry('file2'),
		entry('b', { type: 'dir' }),
		entry('Zed'),
		entry('link', { type: 'symlink', targetType: 'dir' }),
		entry('a', { type: 'dir' })
	];
	expect(names(sortEntries(list, { key: 'name', dir: 1 }))).toEqual([
		'a',
		'b',
		'link',
		'file2',
		'file10',
		'Zed'
	]);
	// Reversed, folders still come first.
	expect(names(sortEntries(list, { key: 'name', dir: -1 }))).toEqual([
		'link',
		'b',
		'a',
		'Zed',
		'file10',
		'file2'
	]);
});

test('size and time sort by the column, and ties by name', () => {
	const list = [
		entry('big', { size: 9, modifiedAt: '2026-03-01T00:00:00Z' }),
		entry('small', { size: 1, modifiedAt: '2026-02-01T00:00:00Z' }),
		entry('also-small', { size: 1, modifiedAt: '2026-01-01T00:00:00Z' })
	];
	expect(names(sortEntries(list, { key: 'size', dir: 1 }))).toEqual(['also-small', 'small', 'big']);
	expect(names(sortEntries(list, { key: 'mtime', dir: -1 }))).toEqual([
		'big',
		'small',
		'also-small'
	]);
});

test('the input is left alone', () => {
	const list = [entry('b'), entry('a')];
	sortEntries(list, { key: 'name', dir: 1 });
	expect(names(list)).toEqual(['b', 'a']);
});

test('clicking a header', () => {
	expect(nextSort({ key: 'name', dir: 1 }, 'name')).toEqual({ key: 'name', dir: -1 });
	expect(nextSort({ key: 'name', dir: 1 }, 'size')).toEqual({ key: 'size', dir: -1 });
	expect(nextSort({ key: 'size', dir: -1 }, 'name')).toEqual({ key: 'name', dir: 1 });
});
