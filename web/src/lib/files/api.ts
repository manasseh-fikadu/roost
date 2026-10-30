// The client for a device's files (internal/api/files.go). Failures throw an ApiError, whose
// status tells "no permission" (403) from "gone" (404).

import { ApiError, csrfHeaders, request } from '#lib/api/client.js';
import type {
	ArchiveToken,
	ConflictMode,
	ExistingNames,
	FileBulk,
	FileEntry,
	FileListing,
	FileSearch,
	FileStat,
	FileUpload
} from '#lib/api/types.js';

export type { ConflictMode };

const qs = (params: Record<string, string | number | undefined>) =>
	Object.entries(params)
		.filter(([, v]) => v !== undefined && v !== '')
		.map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`)
		.join('&');

const base = (device: string) => `devices/${encodeURIComponent(device)}/files`;

/** When an entry last changed, in milliseconds since the epoch. */
export function mtimeOf(e: Pick<FileEntry, 'modifiedAt'>): number {
	return Date.parse(e.modifiedAt);
}

/** Where a file's bytes are: shown in the browser if it can be, or with `download`, saved. */
export function rawUrl(device: string, path: string, opts: { download?: boolean } = {}): string {
	return `/api/${base(device)}/raw?${qs({ path, download: opts.download ? 1 : undefined })}`;
}

export const filesApi = {
	list: (device: string, path: string | null) =>
		request<FileListing>('GET', `${base(device)}/list?${qs({ path: path ?? undefined })}`),
	stat: (device: string, path: string) =>
		request<FileStat>('GET', `${base(device)}/stat?${qs({ path })}`),
	search: (device: string, path: string, q: string) =>
		request<FileSearch>('GET', `${base(device)}/search?${qs({ path, q })}`),
	mkdir: (device: string, dir: string, name: string) =>
		request<FileEntry>('POST', `${base(device)}/mkdir`, { dir, name }),
	rename: (device: string, path: string, name: string) =>
		request<FileEntry>('POST', `${base(device)}/rename`, { path, name }),
	move: (device: string, paths: string[], dest: string, conflict: ConflictMode = 'fail') =>
		request<FileBulk>('POST', `${base(device)}/move`, { paths, dest, conflict }),
	copy: (device: string, paths: string[], dest: string, conflict: ConflictMode = 'fail') =>
		request<FileBulk>('POST', `${base(device)}/copy`, { paths, dest, conflict }),
	remove: (device: string, paths: string[]) =>
		request<FileBulk>('POST', `${base(device)}/delete`, { paths }),
	exists: (device: string, dir: string, names: string[]) =>
		request<ExistingNames>('POST', `${base(device)}/exists`, { dir, names }),
	/**
	 * Saves text as a file's content. The server refuses with a 409 if the file changed since
	 * `expectedMtime` (milliseconds), or with `create`, if it exists.
	 */
	async write(
		device: string,
		path: string,
		text: string,
		opts: { expectedMtime?: number; create?: boolean } = {}
	): Promise<FileEntry> {
		const url = `/api/${base(device)}/write?${qs({
			path,
			expectedMtime: opts.expectedMtime,
			create: opts.create ? 1 : undefined
		})}`;
		let res: Response;
		try {
			res = await fetch(url, {
				method: 'PUT',
				headers: { ...csrfHeaders, 'content-type': 'text/plain; charset=utf-8' },
				body: text
			});
		} catch {
			throw new ApiError(0, "Couldn't reach Roost");
		}
		return readJson<FileEntry>(res);
	},
	/** Starts a .tar.gz download of names inside dir. */
	async downloadArchive(device: string, dir: string, names: string[]): Promise<void> {
		if (names.length === 1) {
			return startDownload(`/api/${base(device)}/archive?${qs({ dir, name: names[0] })}`);
		}
		// A long selection doesn't fit in a URL, so the server parks it under a short-lived token.
		const { token } = await request<ArchiveToken>('POST', `${base(device)}/archive`, {
			dir,
			names
		});
		startDownload(`/api/${base(device)}/archive?${qs({ token })}`);
	}
};

async function readJson<T>(res: Response): Promise<T> {
	const data: unknown = await res.json().catch(() => ({}));
	if (!res.ok) {
		const message = (data as { error?: string }).error;
		throw new ApiError(res.status, message ?? `${res.status} ${res.statusText}`);
	}
	return data as T;
}

/**
 * Fetches a file's bytes. `limit` asks for just the first bytes, or with `tail` the last, as a
 * range. `mtime` is in milliseconds and `size` is the whole file's.
 */
export async function fetchRaw(
	device: string,
	path: string,
	opts: { limit?: number; tail?: boolean; signal?: AbortSignal } = {}
): Promise<{ data: ArrayBuffer; mtime: number; size: number }> {
	let res: Response;
	try {
		res = await fetch(rawUrl(device, path), {
			headers: opts.limit
				? { range: opts.tail ? `bytes=-${opts.limit}` : `bytes=0-${opts.limit - 1}` }
				: undefined,
			cache: 'no-store',
			signal: opts.signal
		});
	} catch (err) {
		if (err instanceof Error && err.name === 'AbortError') {
			throw err;
		}
		throw new ApiError(0, "Couldn't reach Roost");
	}
	if (!res.ok && res.status !== 206) {
		return readJson(res);
	}
	return {
		data: await res.arrayBuffer(),
		mtime: Number(res.headers.get('x-file-mtime')),
		size: Number(res.headers.get('x-file-size'))
	};
}

/** Saves what a URL serves, as a click on a link would. */
export function startDownload(url: string): void {
	const a = document.createElement('a');
	a.href = url;
	a.rel = 'noopener';
	a.download = '';
	document.body.appendChild(a);
	a.click();
	a.remove();
}

/** The error from a failed call, as words, with its status. */
export function errorOf(err: unknown): { status: number; message: string } {
	if (err instanceof ApiError) {
		return { status: err.status, message: err.message };
	}
	return { status: 0, message: err instanceof Error ? err.message : String(err) };
}

/**
 * Uploads a file with progress. It rejects with an ApiError, or an AbortError when `signal`
 * aborts. `name` may be a relative path from a folder upload, whose folders the server makes.
 */
export function uploadFile(
	device: string,
	opts: {
		dir: string;
		name: string;
		file: Blob;
		conflict: ConflictMode;
		onProgress: (sent: number) => void;
		signal: AbortSignal;
	}
): Promise<FileUpload> {
	return new Promise((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		xhr.open(
			'PUT',
			`/api/${base(device)}/upload?${qs({ dir: opts.dir, name: opts.name, conflict: opts.conflict })}`
		);
		for (const [k, v] of Object.entries(csrfHeaders)) {
			xhr.setRequestHeader(k, v);
		}
		xhr.upload.onprogress = (e) => opts.onProgress(e.loaded);
		xhr.onload = () => {
			let body: { error?: string } & Partial<FileUpload> = {};
			try {
				body = JSON.parse(xhr.responseText);
			} catch {
				// Not JSON: a proxy's error page.
			}
			if (xhr.status >= 200 && xhr.status < 300) {
				resolve(body as FileUpload);
			} else {
				reject(new ApiError(xhr.status, body.error ?? `${xhr.status} ${xhr.statusText}`));
			}
		};
		xhr.onerror = () => reject(new ApiError(0, 'The connection to Roost dropped'));
		const abort = () => xhr.abort();
		xhr.onabort = () => reject(new DOMException('Upload canceled', 'AbortError'));
		if (opts.signal.aborted) {
			return abort();
		}
		opts.signal.addEventListener('abort', abort, { once: true });
		xhr.send(opts.file);
	});
}
