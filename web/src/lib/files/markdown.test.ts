import { describe, expect, test } from 'bun:test';
import {
	markdownResolver,
	parseInline,
	parseMarkdown,
	resolveRelative,
	type Block,
	type Inline
} from './markdown.js';

// Relative targets sit under /api/raw, as they do in the app.
const resolve = markdownResolver((url) => `/api/raw?path=${encodeURIComponent(url)}`);

const inline = (text: string) => parseInline(text, resolve);
const doc = (text: string) => parseMarkdown(text, resolve);
const t = (text: string): Inline => ({ type: 'text', text });

/** Every URL a tree would put in an href or src, so a test can check that none is unsafe. */
function urls(nodes: Array<Block | Inline>): string[] {
	const out: string[] = [];
	const walk = (n: unknown) => {
		if (Array.isArray(n)) {
			n.forEach(walk);
		} else if (n && typeof n === 'object') {
			const o = n as Record<string, unknown>;
			if (typeof o.href === 'string') out.push(o.href);
			if (typeof o.src === 'string') out.push(o.src);
			Object.values(o).forEach(walk);
		}
	};
	walk(nodes);
	return out;
}

describe('resolver', () => {
	test.each([
		['https://example.com/a', 'link', 'https://example.com/a'],
		['http://example.com', 'link', 'http://example.com'],
		['HTTPS://EXAMPLE.COM', 'link', 'HTTPS://EXAMPLE.COM'],
		['mailto:me@example.com', 'link', 'mailto:me@example.com'],
		['javascript:alert(1)', 'link', null],
		['JaVaScRiPt:alert(1)', 'link', null],
		['\u0001javascript:alert(1)', 'link', null],
		['java\tscript:alert(1)', 'link', null],
		['data:text/html,<script>alert(1)</script>', 'link', null],
		['file:///etc/passwd', 'link', null],
		['vbscript:x', 'link', null],
		['//evil.example/x', 'link', null],
		['#anchor', 'link', null],
		['docs/a.md', 'link', '/api/raw?path=docs%2Fa.md'],
		['./a.png', 'image', '/api/raw?path=.%2Fa.png'],
		['https://example.com/a.png', 'image', 'https://example.com/a.png'],
		['http://example.com/a.png', 'image', null],
		['mailto:me@example.com', 'image', null],
		['data:image/png;base64,AAAA', 'image', null]
	] as const)('%j as a %s is %j', (url, kind, want) => {
		expect(resolve(url, kind)).toBe(want);
	});

	test('only a path on this origin may come back from the relative resolver', () => {
		for (const bad of [
			'//evil.example',
			'/\\evil.example',
			'https://evil.example',
			'javascript:x'
		]) {
			expect(markdownResolver(() => bad)('a.md', 'link')).toBeNull();
		}
		expect(markdownResolver(() => null)('a.md', 'link')).toBeNull();
	});
});

describe('resolveRelative', () => {
	test.each([
		['/docs', 'a.png', '/docs/a.png'],
		['/docs', '../img/a.png', '/img/a.png'],
		['/docs', '/abs/a.png', '/abs/a.png'],
		['/docs/sub', './x/../y.md', '/docs/sub/y.md'],
		['/', 'a.png', '/a.png'],
		['/', '../../a.png', '/a.png'],
		['/docs', 'a.png?raw=1#top', '/docs/a.png'],
		['/my docs', 'my pic.png', '/my docs/my pic.png'],
		['/docs', 'a%20b.png', '/docs/a b.png'],
		['/a#b/c', 'x.png', '/a#b/c/x.png']
	])('%s + %s is %s', (dir, rel, want) => {
		expect(resolveRelative(dir, rel)).toBe(want);
	});

	test('a malformed escape gives null instead of throwing', () => {
		expect(resolveRelative('/docs', '%E0%A4%A.png')).toBeNull();
	});
});

describe('inline', () => {
	test('plain text and escaped HTML stay text', () => {
		expect(inline('hello <script>alert(1)</script> & <b>x</b>')).toEqual([
			t('hello <script>alert(1)</script> & <b>x</b>')
		]);
	});

	test('emphasis, strong and strikethrough', () => {
		expect(inline('a *b* **c** ~~d~~ _e_ __f__')).toEqual([
			t('a '),
			{ type: 'em', children: [t('b')] },
			t(' '),
			{ type: 'strong', children: [t('c')] },
			t(' '),
			{ type: 'del', children: [t('d')] },
			t(' '),
			{ type: 'em', children: [t('e')] },
			t(' '),
			{ type: 'strong', children: [t('f')] }
		]);
	});

	test('marks nest', () => {
		expect(inline('**a *b* c**')).toEqual([
			{ type: 'strong', children: [t('a '), { type: 'em', children: [t('b')] }, t(' c')] }
		]);
	});

	test.each([
		['snake_case_name'],
		['2 ** 3 ** 4'],
		['a * b * c'],
		['unclosed *star'],
		['unclosed **strong'],
		['lone ~tilde~'],
		['trailing mark*'],
		['file__init__.py']
	])('%s has no emphasis', (text) => {
		expect(inline(text)).toEqual([t(text)]);
	});

	test('a single mark does not close on half of a double one', () => {
		expect(inline('*a **b** c*')).toEqual([
			{
				type: 'em',
				children: [t('a '), { type: 'strong', children: [t('b')] }, t(' c')]
			}
		]);
	});

	test('code spans keep their contents as they are', () => {
		expect(inline('use `a *b* <i>` here')).toEqual([
			t('use '),
			{ type: 'code', text: 'a *b* <i>' },
			t(' here')
		]);
		expect(inline('x ``a ` b`` y')).toEqual([t('x '), { type: 'code', text: 'a ` b' }, t(' y')]);
		expect(inline('``` ```')).toEqual([t('``` ```')]);
		expect(inline('an `unclosed span')).toEqual([t('an `unclosed span')]);
	});

	test('backslash escapes', () => {
		expect(inline('\\*not em\\* and \\_x\\_ and \\\\ and \\q')).toEqual([
			t('*not em* and _x_ and \\ and \\q')
		]);
	});

	test('links', () => {
		expect(inline('see [the **docs**](https://example.com/a "title") now')).toEqual([
			t('see '),
			{
				type: 'link',
				href: 'https://example.com/a',
				children: [t('the '), { type: 'strong', children: [t('docs')] }]
			},
			t(' now')
		]);
		expect(inline('[a](<docs/a.md>)')).toEqual([
			{ type: 'link', href: '/api/raw?path=docs%2Fa.md', children: [t('a')] }
		]);
		expect(inline('<https://example.com/x>')).toEqual([
			{ type: 'link', href: 'https://example.com/x', children: [t('https://example.com/x')] }
		]);
	});

	test('a link that is refused keeps its text and loses its target', () => {
		// The target ends at the first ")", so the last one is text.
		expect(inline('[click](javascript:alert(1))')).toEqual([
			{ type: 'link', href: null, children: [t('click')] },
			t(')')
		]);
		expect(inline('[click](javascript:alert)')).toEqual([
			{ type: 'link', href: null, children: [t('click')] }
		]);
		expect(inline('[top](#top)')).toEqual([{ type: 'link', href: null, children: [t('top')] }]);
		expect(inline('<http://[::1]/x>')).toEqual([
			{ type: 'link', href: 'http://[::1]/x', children: [t('http://[::1]/x')] }
		]);
	});

	test('images load only from https or the same machine', () => {
		expect(inline('![logo](https://example.com/a.png)')).toEqual([
			{ type: 'image', src: 'https://example.com/a.png', alt: 'logo' }
		]);
		expect(inline('![logo](img/a.png)')).toEqual([
			{ type: 'image', src: '/api/raw?path=img%2Fa.png', alt: 'logo' }
		]);
		for (const src of ['http://example.com/a.png', 'data:image/svg+xml,x', 'javascript:x']) {
			const [node] = inline(`![logo](${src})`);
			expect(node).toEqual({ type: 'image', src: null, alt: 'logo' });
		}
	});

	test('an image in a link, such as a badge', () => {
		expect(inline('[![build](https://ci.example/b.svg)](https://ci.example/run)')).toEqual([
			{
				type: 'link',
				href: 'https://ci.example/run',
				children: [{ type: 'image', src: 'https://ci.example/b.svg', alt: 'build' }]
			}
		]);
	});

	test('no URL in any tree is unsafe', () => {
		const hostile = [
			'[a](javascript:alert(1))',
			'[a](JAVASCRIPT:alert\\(1\\))',
			'[a](data:text/html;base64,AAAA)',
			'[a](\u0001javascript:alert\\(1\\))',
			'![a](http://tracker.example/p.gif)',
			'![a](data:image/gif;base64,AAAA)',
			'<javascript:alert(1)>',
			'[a](//evil.example)',
			'[a](/\\evil.example)',
			'[a](https://ok.example)',
			'![a](https://ok.example/p.png)',
			'[a](../x.md)'
		];
		for (const h of hostile) {
			for (const u of urls(inline(h))) {
				expect(u).toMatch(/^(https:\/\/|http:\/\/|mailto:|\/api\/raw\?path=)/i);
			}
		}
	});

	test('depth is bounded', () => {
		const deep = `${'['.repeat(200)}x${']'.repeat(200)}(u)`;
		expect(() => inline(deep)).not.toThrow();
		expect(() => inline('*a '.repeat(5000))).not.toThrow();
	});
});

describe('blocks', () => {
	test('headings and the hashes that close them', () => {
		const [h1, h2, h3, h4, h5, h6] = doc('# One\n## Two ##\n### C#\n#### #hash\n##### 5\n###### 6');
		expect(h1).toEqual({ type: 'heading', level: 1, inline: [t('One')] });
		expect(h2).toEqual({ type: 'heading', level: 2, inline: [t('Two')] });
		expect(h3).toEqual({ type: 'heading', level: 3, inline: [t('C#')] });
		expect(h4).toEqual({ type: 'heading', level: 4, inline: [t('#hash')] });
		expect(h5).toMatchObject({ level: 5 });
		expect(h6).toMatchObject({ level: 6 });
	});

	test('"#hashtag" and seven hashes are paragraphs', () => {
		expect(doc('#hashtag')).toEqual([{ type: 'paragraph', inline: [t('#hashtag')] }]);
		expect(doc('####### seven')[0].type).toBe('paragraph');
	});

	test('setext headings', () => {
		expect(doc('Title\n=====\n\nSub\n---\n\ntext')).toEqual([
			{ type: 'heading', level: 1, inline: [t('Title')] },
			{ type: 'heading', level: 2, inline: [t('Sub')] },
			{ type: 'paragraph', inline: [t('text')] }
		]);
	});

	test('paragraphs join their lines and end at a blank line', () => {
		expect(doc('a\nb\n\nc')).toEqual([
			{ type: 'paragraph', inline: [t('a\nb')] },
			{ type: 'paragraph', inline: [t('c')] }
		]);
	});

	test('rules, including one right after a paragraph', () => {
		expect(doc('---\n***\n_ _ _\na\n***')).toEqual([
			{ type: 'rule' },
			{ type: 'rule' },
			{ type: 'rule' },
			{ type: 'paragraph', inline: [t('a')] },
			{ type: 'rule' }
		]);
	});

	test('raw HTML is a paragraph of text', () => {
		expect(doc('<div onclick="x()">\n<script>alert(1)</script>')).toEqual([
			{ type: 'paragraph', inline: [t('<div onclick="x()">\n<script>alert(1)</script>')] }
		]);
	});

	test('code fences keep everything, including markup', () => {
		expect(doc('```ts\nconst a = *b*;\n# not a heading\n```\nafter')).toEqual([
			{ type: 'code', text: 'const a = *b*;\n# not a heading', lang: 'ts' },
			{ type: 'paragraph', inline: [t('after')] }
		]);
		expect(doc('~~~\na\n~~~')).toEqual([{ type: 'code', text: 'a', lang: '' }]);
	});

	test('a fence closes only on a long enough fence of its own kind', () => {
		expect(doc('````md\n```\ninner\n```\n````\nx')).toEqual([
			{ type: 'code', text: '```\ninner\n```', lang: 'md' },
			{ type: 'paragraph', inline: [t('x')] }
		]);
		expect(doc('```\na\n~~~\nb\n```')).toEqual([{ type: 'code', text: 'a\n~~~\nb', lang: '' }]);
	});

	test('an unclosed fence runs to the end', () => {
		expect(doc('```\na\nb')).toEqual([{ type: 'code', text: 'a\nb', lang: '' }]);
	});

	test('quotes hold blocks', () => {
		expect(doc('> # Title\n> text\n>\n> - item')).toEqual([
			{
				type: 'quote',
				blocks: [
					{ type: 'heading', level: 1, inline: [t('Title')] },
					{ type: 'paragraph', inline: [t('text')] },
					{
						type: 'list',
						ordered: false,
						start: 1,
						items: [{ checked: null, inline: [t('item')], blocks: [] }]
					}
				]
			}
		]);
	});

	test('lists: bullets, numbers from where they start, tasks and nesting', () => {
		expect(doc('- a\n- b\n  - c\n  - d\n- e')).toEqual([
			{
				type: 'list',
				ordered: false,
				start: 1,
				items: [
					{ checked: null, inline: [t('a')], blocks: [] },
					{
						checked: null,
						inline: [t('b')],
						blocks: [
							{
								type: 'list',
								ordered: false,
								start: 1,
								items: [
									{ checked: null, inline: [t('c')], blocks: [] },
									{ checked: null, inline: [t('d')], blocks: [] }
								]
							}
						]
					},
					{ checked: null, inline: [t('e')], blocks: [] }
				]
			}
		]);
		const [numbered] = doc('3. x\n4. y');
		expect(numbered).toMatchObject({ type: 'list', ordered: true, start: 3 });
		const [tasks] = doc('- [ ] open\n- [x] done\n- [X] also');
		expect(tasks).toMatchObject({
			items: [
				{ checked: false, inline: [t('open')] },
				{ checked: true, inline: [t('done')] },
				{ checked: true, inline: [t('also')] }
			]
		});
	});

	test('numbers after bullets are another list', () => {
		const blocks = doc('- a\n- b\n\n3. c\n4. d\n\n- e');
		expect(blocks.map((b) => (b.type === 'list' ? [b.ordered, b.items.length] : b.type))).toEqual([
			[false, 2],
			[true, 2],
			[false, 1]
		]);
	});

	test('a list item can hold a paragraph and code', () => {
		expect(doc('1. step\n\n   more words\n\n   ```\n   code\n   ```\n2. next')).toEqual([
			{
				type: 'list',
				ordered: true,
				start: 1,
				items: [
					{
						checked: null,
						inline: [t('step')],
						blocks: [
							{ type: 'paragraph', inline: [t('more words')] },
							{ type: 'code', text: 'code', lang: '' }
						]
					},
					{ checked: null, inline: [t('next')], blocks: [] }
				]
			}
		]);
	});

	test('a list interrupts a paragraph, a bullet-looking word does not', () => {
		expect(doc('intro\n- a')).toEqual([
			{ type: 'paragraph', inline: [t('intro')] },
			{
				type: 'list',
				ordered: false,
				start: 1,
				items: [{ checked: null, inline: [t('a')], blocks: [] }]
			}
		]);
		expect(doc('-not a list')).toEqual([{ type: 'paragraph', inline: [t('-not a list')] }]);
	});

	test('tables', () => {
		const [table] = doc('| Name | Qty | Note |\n|:--|--:|:-:|\n| a | 1 | x \\| y |\n| b | 2 |');
		expect(table).toEqual({
			type: 'table',
			align: ['left', 'right', 'center'],
			head: [[t('Name')], [t('Qty')], [t('Note')]],
			rows: [
				[[t('a')], [t('1')], [t('x | y')]],
				[[t('b')], [t('2')], []]
			]
		});
	});

	test('a table without edge pipes, and cells with markup', () => {
		const [table] = doc('a | b\n--|--\n**x** | `y`');
		expect(table).toMatchObject({
			type: 'table',
			rows: [[[{ type: 'strong' }], [{ type: 'code', text: 'y' }]]]
		});
	});

	test('a pipe line over a separator of another width is not a table', () => {
		expect(doc('a | b\n---')[0].type).not.toBe('table');
	});

	test('windows line endings and a byte order mark', () => {
		expect(doc('﻿# Hi\r\n\r\ntext\r\nmore')).toEqual([
			{ type: 'heading', level: 1, inline: [t('Hi')] },
			{ type: 'paragraph', inline: [t('text\nmore')] }
		]);
	});

	test('empty and blank documents', () => {
		expect(doc('')).toEqual([]);
		expect(doc('\n  \n\t\n')).toEqual([]);
	});
});

describe('hostile input', () => {
	test('deeply nested quotes and lists do not overflow the stack', () => {
		expect(() => doc(`${'>'.repeat(20000)} x`)).not.toThrow();
		const lists = Array.from({ length: 300 }, (_, n) => `${' '.repeat(2 * n)}- x`).join('\n');
		expect(() => doc(lists)).not.toThrow();
	});

	test('quotes stop nesting at a fixed depth', () => {
		let blocks = doc(`${'> '.repeat(30)}x`);
		let depth = 0;
		while (blocks[0]?.type === 'quote') {
			blocks = (blocks[0] as Extract<Block, { type: 'quote' }>).blocks;
			depth++;
		}
		expect(depth).toBeLessThan(30);
		expect(blocks[0]?.type).toBe('paragraph');
	});

	test('runs of marks finish quickly', () => {
		const started = performance.now();
		for (const unit of [
			'*a ',
			'**a ',
			'__a ',
			'~~a ',
			'`',
			'[',
			'![',
			'[a](',
			'<https://',
			'_a '
		]) {
			doc(unit.repeat(20000));
		}
		doc('`'.repeat(50000));
		doc(`${' '.repeat(100000)}x`);
		doc(`# ${' '.repeat(100000)}x ${'#'.repeat(1000)}`);
		doc(`| a |\n${'|-'.repeat(20000)}`);
		expect(performance.now() - started).toBeLessThan(4000);
	});
});
