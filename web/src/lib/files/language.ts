// Picks the syntax highlighting for a file from its name, from CodeMirror's table of languages.

import { languages } from '@codemirror/language-data';
import { extOf } from './kinds.js';

export type Language = (typeof languages)[number];

/** Extensions CodeMirror has no grammar for, mapped to the closest language it does. */
const ALIASES: Record<string, string> = {
	conf: 'Properties files',
	cfg: 'Properties files',
	cnf: 'Properties files',
	service: 'Properties files',
	timer: 'Properties files',
	socket: 'Properties files',
	mount: 'Properties files',
	target: 'Properties files',
	network: 'Properties files',
	desktop: 'Properties files',
	editorconfig: 'Properties files',
	env: 'Shell',
	bashrc: 'Shell',
	zshrc: 'Shell',
	profile: 'Shell',
	bash_profile: 'Shell',
	zsh: 'Shell',
	fish: 'Shell',
	envrc: 'Shell',
	gitignore: 'Shell',
	dockerignore: 'Shell',
	jsonc: 'JSON',
	json5: 'JSON',
	jsonl: 'JSON',
	ndjson: 'JSON',
	geojson: 'JSON',
	webmanifest: 'JSON',
	mdx: 'Markdown',
	containerfile: 'Dockerfile',
	dockerfile: 'Dockerfile',
	plist: 'XML',
	csproj: 'XML'
};

/** The language of a file by its name, or null for plain text. */
export function findLanguage(name: string): Language | null {
	for (const l of languages) {
		if (l.filename?.test(name)) {
			return l;
		}
	}
	const ext = extOf(name);
	if (!ext) {
		return /^(containerfile|dockerfile)/i.test(name)
			? (languages.find((l) => l.name === 'Dockerfile') ?? null)
			: null;
	}
	return (
		languages.find((l) => l.extensions.includes(ext)) ??
		languages.find((l) => l.name === ALIASES[ext]) ??
		null
	);
}

/** Whether a file opens with long lines wrapped: prose does, code doesn't. */
export function wrapsByDefault(name: string): boolean {
	const ext = extOf(name);
	return !ext || ['md', 'markdown', 'txt', 'log', 'csv', 'tsv', 'rst', 'tex'].includes(ext);
}
