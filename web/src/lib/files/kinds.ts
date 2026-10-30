// What a file is for, by its name: it picks the icon and which preview opens. The server has the
// same table (internal/files/kinds.go), which decides what it may serve inline.

import type { FileEntry } from '#lib/api/types.js';

export type FileKind =
	'image' | 'video' | 'audio' | 'pdf' | 'text' | 'docx' | 'sheet' | 'archive' | 'other';

const KINDS: Record<Exclude<FileKind, 'other'>, string> = {
	image: 'png jpg jpeg jfif gif webp avif bmp ico svg apng',
	video: 'mp4 m4v webm ogv mov mkv',
	audio: 'mp3 wav ogg oga opus m4a aac flac weba',
	pdf: 'pdf',
	docx: 'docx docm dotx',
	sheet: 'xlsx xlsm xlsb xls ods csv tsv numbers',
	archive: 'zip tar gz tgz bz2 xz zst 7z rar deb rpm dmg iso jar apk',
	text: `txt text md markdown mdx rst adoc org tex log out err
		json jsonc json5 jsonl ndjson geojson yaml yml toml ini cfg conf cnf config properties env editorconfig
		xml xsd xsl plist html htm xhtml css scss sass less styl
		js mjs cjs jsx ts mts cts tsx vue svelte astro
		py pyi rb go rs java kt kts scala groovy gradle swift m mm c h cc cpp cxx hpp hh cs fs vb
		php pl pm lua r jl dart ex exs erl hrl clj cljs elm hs ml mli nim zig v sol
		sh bash zsh fish ksh ps1 psm1 bat cmd awk sed
		sql graphql gql proto prisma tf tfvars hcl nix cmake mk make dockerfile containerfile
		service timer socket mount target path network netdev link rules desktop
		diff patch srt vtt ass nfo lock sum mod pem crt cer csr pub asc gpg-key
		gitignore gitattributes gitmodules dockerignore npmrc nvmrc htaccess bashrc zshrc profile vimrc tmux
		csproj sln pbxproj xcconfig entitlements rc list repo sources pref manifest webmanifest map`
};

const KIND_BY_EXT = new Map<string, FileKind>();
for (const [kind, exts] of Object.entries(KINDS)) {
	for (const ext of exts.split(/\s+/)) {
		KIND_BY_EXT.set(ext, kind as FileKind);
	}
}

/** Files without an extension that are almost always text. */
const TEXT_NAMES = new Set(
	`dockerfile containerfile makefile gnumakefile rakefile gemfile procfile vagrantfile jenkinsfile caddyfile justfile brewfile
	readme license copying authors contributors changelog changes news todo notice install
	hosts hostname fstab crontab passwd group shells resolv.conf sudoers environment motd issue timezone machine-id
	authorized_keys known_hosts config id_rsa.pub id_ed25519.pub`.split(/\s+/)
);

/** The extension of a name in lower case, without the dot. ".bashrc" is all extension. */
export function extOf(name: string): string {
	const lower = name.toLowerCase();
	const dot = lower.lastIndexOf('.');
	return dot > 0 ? lower.slice(dot + 1) : dot === 0 ? lower.slice(1) : '';
}

export function fileKind(name: string): FileKind {
	const lower = name.toLowerCase();
	const byExt = KIND_BY_EXT.get(extOf(lower));
	if (byExt) {
		return byExt;
	}
	if (
		TEXT_NAMES.has(lower) ||
		/^(readme|license|changelog)\b/.test(lower) ||
		/\.(log|conf)\.\d+$/.test(lower)
	) {
		return 'text';
	}
	return 'other';
}

/** Whether an entry is a folder, or a link to one, which opens like one. */
export function isDirLike(e: Pick<FileEntry, 'type' | 'targetType'>): boolean {
	return e.type === 'dir' || (e.type === 'symlink' && e.targetType === 'dir');
}

export type PreviewKind =
	'image' | 'video' | 'audio' | 'pdf' | 'text' | 'markdown' | 'docx' | 'sheet' | 'other';

/** Which preview opens for a file. Archives, and the spreadsheets the browser can't read, have none. */
export function previewKind(name: string): PreviewKind {
	const ext = extOf(name);
	if (ext === 'md' || ext === 'markdown' || ext === 'mdx') {
		return 'markdown';
	}
	if (ext === 'numbers' || ext === 'xlsb') {
		return 'other';
	}
	const kind = fileKind(name);
	return kind === 'archive' ? 'other' : kind;
}

/** Whether the server shows a file inline, and so lets it open in a new tab. */
export function canInline(name: string): boolean {
	const kind = fileKind(name);
	const ext = extOf(name);
	return (
		kind === 'text' ||
		ext === 'csv' ||
		ext === 'tsv' ||
		kind === 'image' ||
		kind === 'video' ||
		kind === 'audio' ||
		kind === 'pdf'
	);
}

/** Text files above this open read-only from a partial read, or download. */
export const TEXT_LIMIT = 5 * 1024 * 1024;
/** How much of a large text file a partial read shows. */
export const TEXT_PARTIAL = 1024 * 1024;

/** A heuristic for files with no known extension: no NUL bytes and valid UTF-8 means text. */
export function looksLikeText(bytes: Uint8Array): boolean {
	const sample = bytes.subarray(0, 8192);
	if (sample.includes(0)) {
		return false;
	}
	try {
		new TextDecoder('utf-8', { fatal: true }).decode(
			sample.length < bytes.length ? trimPartialChar(sample) : sample
		);
		return true;
	} catch {
		return false;
	}
}

/** Drops a multi-byte UTF-8 sequence cut off at the end of a sample. */
function trimPartialChar(b: Uint8Array): Uint8Array {
	let i = b.length - 1;
	let back = 0;
	while (i >= 0 && back < 4 && (b[i] & 0xc0) === 0x80) {
		i--;
		back++;
	}
	return i >= 0 && b[i] >= 0xc0 ? b.subarray(0, i) : b;
}
