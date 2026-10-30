import { describe, expect, test } from 'bun:test';
import { extOf, fileKind, isDirLike, looksLikeText, previewKind } from './kinds.js';

describe('fileKind', () => {
	test.each([
		['photo.JPG', 'image'],
		['clip.mp4', 'video'],
		['song.flac', 'audio'],
		['doc.pdf', 'pdf'],
		['main.go', 'text'],
		['Dockerfile', 'text'],
		['.bashrc', 'text'],
		['README', 'text'],
		['app.log.1', 'text'],
		['nginx.conf.2', 'text'],
		['notes.docx', 'docx'],
		['data.csv', 'sheet'],
		['a.tar.gz', 'archive'],
		['program', 'other'],
		['', 'other'],
		['a.unknownext', 'other']
	])('%s is %s', (name, kind) => {
		expect(fileKind(name)).toBe(kind as ReturnType<typeof fileKind>);
	});
});

test('extOf', () => {
	expect(extOf('a.TXT')).toBe('txt');
	expect(extOf('.env')).toBe('env');
	expect(extOf('noext')).toBe('');
	expect(extOf('a.b.c')).toBe('c');
});

test('previewKind folds markdown out of text and hides what the browser cannot read', () => {
	expect(previewKind('a.md')).toBe('markdown');
	expect(previewKind('a.txt')).toBe('text');
	expect(previewKind('a.zip')).toBe('other');
	expect(previewKind('a.numbers')).toBe('other');
	expect(previewKind('a.xlsb')).toBe('other');
	expect(previewKind('a.xlsx')).toBe('sheet');
});

test('isDirLike follows a link to a folder', () => {
	expect(isDirLike({ type: 'dir' })).toBe(true);
	expect(isDirLike({ type: 'symlink', targetType: 'dir' })).toBe(true);
	expect(isDirLike({ type: 'symlink', targetType: 'file' })).toBe(false);
	expect(isDirLike({ type: 'symlink' })).toBe(false);
	expect(isDirLike({ type: 'file' })).toBe(false);
});

describe('looksLikeText', () => {
	const bytes = (s: string) => new TextEncoder().encode(s);
	test('plain and unicode text', () => {
		expect(looksLikeText(bytes('hello\nworld'))).toBe(true);
		expect(looksLikeText(bytes('työ ☃ 🐓'))).toBe(true);
		expect(looksLikeText(new Uint8Array())).toBe(true);
	});
	test('NUL bytes and invalid UTF-8 are binary', () => {
		expect(looksLikeText(new Uint8Array([104, 0, 105]))).toBe(false);
		expect(looksLikeText(new Uint8Array([0xff, 0xfe, 0x41]))).toBe(false);
	});
	test('a character cut off at the end of a sample is still text', () => {
		const full = bytes('é'.repeat(5000));
		expect(looksLikeText(full)).toBe(true);
	});
});
