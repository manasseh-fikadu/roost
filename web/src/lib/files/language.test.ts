import { describe, expect, test } from 'bun:test';
import { findLanguage, wrapsByDefault } from './language.js';

describe('findLanguage', () => {
	test.each([
		['main.go', 'Go'],
		['app.ts', 'TypeScript'],
		['script.py', 'Python'],
		['README.md', 'Markdown'],
		['notes.MDX', 'Markdown'],
		['Dockerfile', 'Dockerfile'],
		['Containerfile', 'Dockerfile'],
		['app.Dockerfile', 'Dockerfile'],
		['nginx.conf', 'Nginx'],
		['config.json', 'JSON'],
		['tsconfig.jsonc', 'JSON'],
		['events.ndjson', 'JSON'],
		['.env', 'Shell'],
		['.bashrc', 'Shell'],
		['.gitignore', 'Shell'],
		['run.sh', 'Shell'],
		['app.conf', 'Properties files'],
		['sshd.service', 'Properties files'],
		['Info.plist', 'XML'],
		['app.csproj', 'XML']
	])('%s is %s', (name, want) => {
		expect(findLanguage(name)?.name).toBe(want);
	});

	test.each([['README'], ['LICENSE'], ['notes.txt'], ['data.unknownext'], ['']])(
		'%j is plain text',
		(name) => {
			expect(findLanguage(name)).toBeNull();
		}
	);

	test('a grammar loads', async () => {
		const support = await findLanguage('main.go')!.load();
		expect(support.language.name).toBe('go');
	});
});

test('prose wraps by default and code does not', () => {
	for (const name of ['a.md', 'a.markdown', 'notes.txt', 'app.log', 'a.csv', 'README', 'LICENSE']) {
		expect(wrapsByDefault(name)).toBe(true);
	}
	for (const name of ['a.go', 'a.ts', 'a.json', 'a.yaml', '.bashrc']) {
		expect(wrapsByDefault(name)).toBe(false);
	}
});
