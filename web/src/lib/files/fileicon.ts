import File from '@lucide/svelte/icons/file';
import FileArchive from '@lucide/svelte/icons/file-archive';
import FileAudio from '@lucide/svelte/icons/file-audio';
import FileCode from '@lucide/svelte/icons/file-code';
import FileImage from '@lucide/svelte/icons/file-image';
import FileJson from '@lucide/svelte/icons/file-json';
import FileKey from '@lucide/svelte/icons/file-key';
import FileQuestion from '@lucide/svelte/icons/file-question-mark';
import FileSpreadsheet from '@lucide/svelte/icons/file-spreadsheet';
import FileSymlink from '@lucide/svelte/icons/file-symlink';
import FileTerminal from '@lucide/svelte/icons/file-terminal';
import FileText from '@lucide/svelte/icons/file-text';
import FileType from '@lucide/svelte/icons/file-type';
import FileVideo from '@lucide/svelte/icons/file-video';
import Folder from '@lucide/svelte/icons/folder';
import FolderSymlink from '@lucide/svelte/icons/folder-symlink';
import type { FileEntry } from '#lib/api/types.js';
import type { Icon } from '#lib/icon.js';
import { extOf, fileKind } from './kinds.js';

const words = (s: string) => new Set(s.split(/\s+/));

const CODE = words(
	`js mjs cjs jsx ts mts cts tsx vue svelte astro py rb go rs java kt scala swift c h cc cpp hpp cs php pl lua r dart ex exs hs zig sql html htm css scss less xml graphql proto`
);
const SHELL = words('sh bash zsh fish ps1 bat cmd');
const DATA = words('json jsonc json5 jsonl ndjson yaml yml toml ini env conf cfg properties');
const KEYS = words('pem crt cer csr pub key asc gpg p12 pfx');

/** The icon for an entry, and the classes that color it. */
export function fileIcon(entry: Pick<FileEntry, 'name' | 'type' | 'targetType'>): {
	icon: Icon;
	tone: string;
} {
	if (entry.type === 'dir') {
		return { icon: Folder, tone: 'fill-blue-fg/15 text-blue-fg' };
	}
	if (entry.type === 'symlink') {
		if (entry.targetType === 'dir') {
			return { icon: FolderSymlink, tone: 'fill-blue-fg/15 text-blue-fg' };
		}
		if (!entry.targetType) {
			return { icon: FileSymlink, tone: 'text-red-fg/80' };
		}
	}
	if (entry.type === 'other') {
		return { icon: FileQuestion, tone: 'text-fg-faint' };
	}
	const ext = extOf(entry.name);
	const kind = fileKind(entry.name);
	if (kind === 'image') return { icon: FileImage, tone: 'text-purple-fg' };
	if (kind === 'video') return { icon: FileVideo, tone: 'text-[#ff8a65]' };
	if (kind === 'audio') return { icon: FileAudio, tone: 'text-[#f472b6]' };
	if (kind === 'pdf') return { icon: FileText, tone: 'text-red-fg' };
	if (kind === 'docx' || ext === 'doc' || ext === 'odt' || ext === 'rtf') {
		return { icon: FileType, tone: 'text-blue-fg' };
	}
	if (kind === 'sheet') return { icon: FileSpreadsheet, tone: 'text-green-fg' };
	if (kind === 'archive') return { icon: FileArchive, tone: 'text-amber-fg' };
	if (KEYS.has(ext) || /^id_(rsa|ed25519|ecdsa)/.test(entry.name)) {
		return { icon: FileKey, tone: 'text-amber-fg' };
	}
	if (SHELL.has(ext)) return { icon: FileTerminal, tone: 'text-green-fg' };
	if (DATA.has(ext)) return { icon: FileJson, tone: 'text-amber-fg/90' };
	if (CODE.has(ext)) return { icon: FileCode, tone: 'text-[#7dd3fc]' };
	if (kind === 'text') return { icon: FileText, tone: 'text-fg-muted' };
	return { icon: File, tone: 'text-fg-subtle' };
}
