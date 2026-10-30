// The upload queue, shared by the file manager and its panel. It lives outside the page, so
// uploads go on while you browse other folders, or other pages of Roost.

import type { ConflictMode } from '#lib/api/types.js';
import { errorOf, uploadFile } from './api.js';

export type UploadStatus = 'queued' | 'uploading' | 'done' | 'error' | 'canceled';

export interface UploadTask {
	id: number;
	device: string;
	dir: string;
	/** The path from dir; a folder upload has subfolders ("photos/a.jpg"). */
	name: string;
	file: File;
	size: number;
	loaded: number;
	status: UploadStatus;
	error: string | null;
	conflict: ConflictMode;
	/** Bytes a second, smoothed. */
	speed: number;
	/** Where the file ended up, which isn't dir/name after "keep both". */
	result: string | null;
}

/** How many files go up at once. */
const PARALLEL = 3;

class Uploads {
	tasks: UploadTask[] = $state([]);
	#next = 1;
	#aborts = new Map<number, AbortController>();
	#done = new Set<(t: UploadTask) => void>();

	/** Calls fn for each file that finishes, such as to refresh the folder it landed in. */
	onUploaded(fn: (t: UploadTask) => void): () => void {
		this.#done.add(fn);
		return () => this.#done.delete(fn);
	}

	enqueue(
		device: string,
		dir: string,
		items: Array<{ file: File; name: string; conflict: ConflictMode }>
	): void {
		for (const it of items) {
			this.tasks.push({
				id: this.#next++,
				device,
				dir,
				name: it.name,
				file: it.file,
				size: it.file.size,
				loaded: 0,
				status: 'queued',
				error: null,
				conflict: it.conflict,
				speed: 0,
				result: null
			});
		}
		this.#pump();
	}

	cancel(id: number): void {
		const t = this.tasks.find((x) => x.id === id);
		if (t?.status === 'uploading') {
			this.#aborts.get(id)?.abort();
		} else if (t?.status === 'queued') {
			t.status = 'canceled';
		}
	}

	cancelAll(): void {
		for (const t of this.tasks) {
			if (t.status === 'queued' || t.status === 'uploading') {
				this.cancel(t.id);
			}
		}
	}

	retry(id: number): void {
		const t = this.tasks.find((x) => x.id === id);
		if (t && (t.status === 'error' || t.status === 'canceled')) {
			Object.assign(t, { status: 'queued', error: null, loaded: 0, speed: 0 });
			this.#pump();
		}
	}

	clearFinished(): void {
		this.tasks = this.tasks.filter((t) => t.status === 'queued' || t.status === 'uploading');
	}

	#pump(): void {
		let running = this.tasks.filter((t) => t.status === 'uploading').length;
		for (const t of this.tasks) {
			if (running >= PARALLEL) {
				break;
			}
			if (t.status === 'queued') {
				running++;
				void this.#start(t);
			}
		}
	}

	async #start(t: UploadTask): Promise<void> {
		const ctl = new AbortController();
		this.#aborts.set(t.id, ctl);
		t.status = 'uploading';
		t.loaded = 0;
		let lastAt = performance.now();
		let lastLoaded = 0;
		try {
			const out = await uploadFile(t.device, {
				dir: t.dir,
				name: t.name,
				file: t.file,
				conflict: t.conflict,
				signal: ctl.signal,
				onProgress: (loaded) => {
					const now = performance.now();
					const dt = (now - lastAt) / 1000;
					// Progress comes every few milliseconds, which is more than anyone can read.
					if (dt < 0.25 && loaded < t.size) {
						return;
					}
					const instant = (loaded - lastLoaded) / Math.max(dt, 0.001);
					lastAt = now;
					lastLoaded = loaded;
					t.loaded = loaded;
					t.speed = t.speed ? t.speed * 0.6 + instant * 0.4 : instant;
				}
			});
			t.loaded = t.size;
			t.result = out.path;
			t.status = 'done';
		} catch (err) {
			if (err instanceof Error && err.name === 'AbortError') {
				t.status = 'canceled';
			} else {
				t.status = 'error';
				t.error = errorOf(err).message;
			}
		}
		t.speed = 0;
		this.#aborts.delete(t.id);
		if (t.status === 'done') {
			for (const fn of this.#done) {
				fn(t);
			}
		}
		this.#pump();
	}
}

export const uploads = new Uploads();
