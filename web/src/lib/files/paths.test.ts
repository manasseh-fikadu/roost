import { expect, test } from 'bun:test';
import { basename, crumbs, dirname, joinPath, splitExt, tildify } from './paths.js';

test('joinPath, dirname and basename', () => {
	expect(joinPath('/', 'a')).toBe('/a');
	expect(joinPath('/a', 'b c')).toBe('/a/b c');
	expect(dirname('/a/b')).toBe('/a');
	expect(dirname('/a')).toBe('/');
	expect(dirname('/')).toBe('/');
	expect(basename('/a/b')).toBe('b');
	expect(basename('/')).toBe('/');
});

test('crumbs', () => {
	expect(crumbs('/')).toEqual([{ name: '/', path: '/' }]);
	expect(crumbs('/a/b')).toEqual([
		{ name: '/', path: '/' },
		{ name: 'a', path: '/a' },
		{ name: 'b', path: '/a/b' }
	]);
});

test('tildify', () => {
	expect(tildify('/home/me', '/home/me')).toBe('~');
	expect(tildify('/home/me/x', '/home/me')).toBe('~/x');
	expect(tildify('/home/meat', '/home/me')).toBe('/home/meat');
	expect(tildify('/x', null)).toBe('/x');
	expect(tildify('/x', '/')).toBe('/x');
});

test('splitExt keeps .tar.gz together and leaves dotfiles alone', () => {
	expect(splitExt('report.final.xlsx')).toEqual(['report.final', '.xlsx']);
	expect(splitExt('backup.tar.gz')).toEqual(['backup', '.tar.gz']);
	expect(splitExt('.bashrc')).toEqual(['.bashrc', '']);
	expect(splitExt('Makefile')).toEqual(['Makefile', '']);
});
