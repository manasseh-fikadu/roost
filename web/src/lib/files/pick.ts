// Collecting the files to upload from a file input, a drop or the clipboard.

export interface Picked {
	file: File;
	/** A relative path, such as "photos/2024/a.jpg" from a folder, or just the file's name. */
	name: string;
}

export function fromInput(list: FileList | null): Picked[] {
	return Array.from(list ?? []).map((file) => ({
		file,
		name: (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name
	}));
}

/**
 * Walks dropped files and folders, and the folders with nothing in them, which the server can
 * only make by name. A browser only hands out a drop's entries during the event, so call this in
 * the handler and wait for the result afterwards.
 */
export function fromDrop(dt: DataTransfer): Promise<{ files: Picked[]; emptyDirs: string[] }> {
	const entries = Array.from(dt.items)
		.filter((i) => i.kind === 'file')
		.map((i) => i.webkitGetAsEntry?.())
		.filter((e): e is FileSystemEntry => Boolean(e));
	const files: Picked[] = entries.length
		? []
		: Array.from(dt.files).map((file) => ({ file, name: file.name }));
	const emptyDirs: string[] = [];

	const readAll = (dir: FileSystemDirectoryEntry) =>
		new Promise<FileSystemEntry[]>((resolve, reject) => {
			const reader = dir.createReader();
			const out: FileSystemEntry[] = [];
			// A call returns at most about a hundred entries, so keep asking until it returns none.
			const next = () =>
				reader.readEntries((batch) => {
					if (!batch.length) {
						return resolve(out);
					}
					out.push(...batch);
					next();
				}, reject);
			next();
		});

	const walk = async (entry: FileSystemEntry, prefix: string): Promise<void> => {
		if (entry.isFile) {
			const file = await new Promise<File>((resolve, reject) =>
				(entry as FileSystemFileEntry).file(resolve, reject)
			);
			files.push({ file, name: prefix + entry.name });
		} else if (entry.isDirectory) {
			const children = await readAll(entry as FileSystemDirectoryEntry);
			if (!children.length) {
				emptyDirs.push(prefix + entry.name);
			}
			for (const c of children) {
				await walk(c, `${prefix}${entry.name}/`);
			}
		}
	};
	return Promise.all(entries.map((e) => walk(e, ''))).then(() => ({ files, emptyDirs }));
}
