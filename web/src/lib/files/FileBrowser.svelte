<!--
	The file manager for one device: places and bookmarks, the path bar, the
	listing with sorting, selection and keyboard, search, drag-and-drop and
	paste uploads, the dialogs and the preview. The folder and the open file
	are in the address, so a link or a reload comes back to the same place.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import AlertTriangle from '@lucide/svelte/icons/triangle-alert';
	import ArrowUp from '@lucide/svelte/icons/arrow-up';
	import ClipboardCopy from '@lucide/svelte/icons/clipboard-copy';
	import Copy from '@lucide/svelte/icons/copy';
	import CopyPlus from '@lucide/svelte/icons/copy-plus';
	import Download from '@lucide/svelte/icons/download';
	import Ellipsis from '@lucide/svelte/icons/ellipsis';
	import Eye from '@lucide/svelte/icons/eye';
	import EyeOff from '@lucide/svelte/icons/eye-off';
	import FilePlus from '@lucide/svelte/icons/file-plus';
	import FolderInput from '@lucide/svelte/icons/folder-input';
	import FolderOpen from '@lucide/svelte/icons/folder-open';
	import FolderPlus from '@lucide/svelte/icons/folder-plus';
	import FolderUp from '@lucide/svelte/icons/folder-up';
	import Lock from '@lucide/svelte/icons/lock';
	import MapPin from '@lucide/svelte/icons/map-pin';
	import Pencil from '@lucide/svelte/icons/pencil';
	import Plus from '@lucide/svelte/icons/plus';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import Search from '@lucide/svelte/icons/search';
	import SquareTerminal from '@lucide/svelte/icons/square-terminal';
	import Star from '@lucide/svelte/icons/star';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import Upload from '@lucide/svelte/icons/upload';
	import X from '@lucide/svelte/icons/x';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import type { ConflictMode, FileEntry, FileListing, FileSearch } from '#lib/api/types.js';
	import { bytes, plural } from '#lib/format.js';
	import { toast } from '#lib/toast.svelte.js';
	import Button from '#lib/ui/Button.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import MenuItem from '#lib/ui/MenuItem.svelte';
	import MenuLabel from '#lib/ui/MenuLabel.svelte';
	import MenuSeparator from '#lib/ui/MenuSeparator.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import { buttonClass } from '#lib/ui/button.js';
	import { errorOf, filesApi, rawUrl, startDownload } from './api.js';
	import { bulkMessage, filesUrl, isHidden, terminalHref, totals } from './browser.js';
	import ContextMenu from './ContextMenu.svelte';
	import DeleteDialog from './DeleteDialog.svelte';
	import DestinationDialog from './DestinationDialog.svelte';
	import { isDirLike } from './kinds.js';
	import Listing from './Listing.svelte';
	import NameDialog from './NameDialog.svelte';
	import PathBar from './PathBar.svelte';
	import { Persisted } from './persisted.svelte.js';
	import { basename, dirname, joinPath, tildify } from './paths.js';
	import { fromDrop, fromInput, type Picked } from './pick.js';
	import Places from './Places.svelte';
	import { quickPlaces } from './places.js';
	import Preview from './Preview.svelte';
	import {
		click,
		emptySelection,
		move,
		toggle,
		toggleAll,
		toggleCursor,
		type Selection
	} from './selection.js';
	import { nextSort, sortEntries, type Sort, type SortKey } from './sort.js';
	import UploadConflictDialog from './UploadConflictDialog.svelte';
	import UploadPanel from './UploadPanel.svelte';
	import { uploads } from './uploads.svelte.js';

	interface Props {
		deviceId: string;
		/** The device's OS, which decides the places it lists. */
		os: string;
	}

	let { deviceId, os }: Props = $props();

	type DialogState =
		| { kind: 'mkdir' }
		| { kind: 'newfile' }
		| { kind: 'rename'; entry: FileEntry }
		| { kind: 'move' | 'copy'; items: FileEntry[] }
		| { kind: 'delete'; items: FileEntry[] };

	interface SearchState {
		root: string;
		query: string;
		result: FileSearch | null;
		error: string | null;
	}

	const pathParam = $derived(page.url.searchParams.get('path'));
	const filePath = $derived(page.url.searchParams.get('file'));
	const requestKey = $derived(`${deviceId}|${pathParam ?? ''}`);

	let now = $state(Date.now());
	let listing = $state.raw<FileListing | null>(null);
	let loadedKey = $state<string | null>(null);
	let error = $state<{ status: number; message: string } | null>(null);
	let refreshing = $state(false);
	let reloadKey = $state(0);

	const showHidden = new Persisted('roost.files.hidden', false);
	const sortPref = new Persisted<Sort>('roost.files.sort', { key: 'name', dir: 1 });
	// A device's bookmarks are its own, so the key has its ID.
	const bookmarkPref = $derived(new Persisted<string[]>(`roost.files.bookmarks.${deviceId}`, []));

	let sel = $state.raw<Selection>(emptySelection());
	let search = $state<SearchState | null>(null);
	let searchOpen = $state(false);
	let searchText = $state('');
	let dialog = $state<DialogState | null>(null);
	let ask = $state<{
		existing: string[];
		total: number;
		run: (m: ConflictMode | 'skip') => void;
	} | null>(null);
	let askOpen = $state(false);
	let dragging = $state(false);
	let statEntry = $state<FileEntry | null>(null);
	let ctx = $state<{ x: number; y: number; entry: FileEntry } | null>(null);
	let ctxOpen = $state(false);
	let placesOpen = $state(false);

	let fileInput = $state<HTMLInputElement>();
	let dirInput = $state<HTMLInputElement>();
	let searchInput = $state<HTMLInputElement>();
	let dragDepth = 0;
	/** The path to put the cursor on once the listing loads, such as a folder just made. */
	let focusPath: string | null = null;
	/** The folder we came up from, so the cursor lands on it. */
	let cameFrom: string | null = null;
	/** Whether the open preview was pushed on the history, so closing it goes back. */
	let pushedPreview = false;

	const current = $derived(listing && loadedKey === requestKey ? listing : null);
	const dir = $derived(current?.path ?? null);
	const home = $derived(listing?.home ?? null);
	const showError = $derived(error !== null && loadedKey === requestKey);

	// ---- loading ----

	function reload() {
		reloadKey++;
	}

	$effect(() => {
		void reloadKey;
		const key = requestKey;
		const id = deviceId;
		const path = pathParam;
		let live = true;
		refreshing = true;
		filesApi
			.list(id, path)
			.then((l) => {
				if (live) {
					listing = l;
					loadedKey = key;
					error = null;
				}
			})
			.catch((err) => {
				if (live) {
					error = errorOf(err);
					loadedKey = key;
				}
			})
			.finally(() => {
				if (live) {
					refreshing = false;
				}
			});
		return () => {
			live = false;
		};
	});

	// A new folder starts with nothing selected and no search.
	$effect(() => {
		void requestKey;
		sel = emptySelection();
		search = null;
		searchOpen = false;
	});

	// The listing's times are relative, and the page may stay open for hours.
	$effect(() => {
		const t = setInterval(() => (now = Date.now()), 30_000);
		return () => clearInterval(t);
	});

	// Coming back to the window often means files changed elsewhere.
	$effect(() => {
		const onVisible = () => document.visibilityState === 'visible' && reload();
		document.addEventListener('visibilitychange', onVisible);
		return () => document.removeEventListener('visibilitychange', onVisible);
	});

	// A finished upload refreshes the folder, once for a burst of them.
	$effect(() => {
		let timer: ReturnType<typeof setTimeout> | undefined;
		const off = uploads.onUploaded((t) => {
			if (t.device === deviceId) {
				clearTimeout(timer);
				timer = setTimeout(reload, 400);
			}
		});
		return () => {
			off();
			clearTimeout(timer);
		};
	});

	const visible = $derived.by(() => {
		if (search?.result) {
			return search.result.entries.filter((e) => showHidden.value || !isHidden(e));
		}
		if (!current) {
			return [];
		}
		return sortEntries(
			current.entries.filter((e) => showHidden.value || !isHidden(e)),
			sortPref.value
		);
	});
	const hiddenCount = $derived(current ? current.entries.filter(isHidden).length : 0);

	// After a rename or a new folder, or going up, put the cursor on the item that matters.
	$effect(() => {
		const rows = visible;
		if (!current) {
			return;
		}
		const target = focusPath ?? cameFrom;
		if (!target) {
			return;
		}
		const i = rows.findIndex((e) => e.path === target || target.startsWith(`${e.path}/`));
		if (i >= 0) {
			untrack(() => {
				sel = focusPath
					? { selected: new Set([rows[i].path]), anchor: i, cursor: i }
					: { ...sel, cursor: i };
			});
		}
		focusPath = null;
		cameFrom = null;
	});

	// ---- navigation ----

	function go(path: string) {
		if (dir) {
			cameFrom = dir;
		}
		void goto(filesUrl(deviceId, path));
	}

	/** A typed path may be a file: open its folder, and the file over it. */
	async function goTyped(path: string) {
		try {
			const { entry } = await filesApi.stat(deviceId, path);
			if (!isDirLike(entry)) {
				void goto(filesUrl(deviceId, dirname(entry.path), entry.path));
				return;
			}
		} catch {
			// Let the listing show what's wrong.
		}
		go(path);
	}

	// With no listing, say for a folder that can't be read, the path asked for still has a parent.
	const parent = $derived(
		current ? current.parent || null : pathParam && pathParam !== '/' ? dirname(pathParam) : null
	);

	function goUp() {
		if (parent) {
			go(parent);
		}
	}

	function openPreview(e: FileEntry) {
		pushedPreview = true;
		void goto(filesUrl(deviceId, pathParam, e.path), { reset: false });
	}

	function open(e: FileEntry) {
		if (isDirLike(e)) {
			go(e.path);
		} else {
			openPreview(e);
		}
	}

	function closePreview() {
		if (pushedPreview) {
			pushedPreview = false;
			history.back();
		} else {
			void goto(filesUrl(deviceId, pathParam), { replace: true, reset: false });
		}
	}

	// The file to preview is in the address: find it in what's loaded, or ask the device.
	const files = $derived(visible.filter((e) => !isDirLike(e)));
	const known = $derived(
		filePath
			? (files.find((e) => e.path === filePath) ??
					current?.entries.find((e) => e.path === filePath) ??
					null)
			: null
	);

	$effect(() => {
		const path = filePath;
		if (!path || known) {
			statEntry = null;
			return;
		}
		let live = true;
		filesApi
			.stat(deviceId, path)
			.then((r) => live && (statEntry = r.entry))
			.catch((err) => {
				if (live) {
					toast(errorOf(err).message, 'error');
					void goto(filesUrl(deviceId, pathParam), { replace: true, reset: false });
				}
			});
		return () => {
			live = false;
		};
	});

	const previewEntry = $derived(known ?? (statEntry?.path === filePath ? statEntry : null));
	const siblings = $derived(
		previewEntry && files.some((f) => f.path === previewEntry.path)
			? files
			: previewEntry
				? [previewEntry]
				: []
	);

	// ---- selection ----

	const selectedEntries = $derived(visible.filter((e) => sel.selected.has(e.path)));
	const selectedSize = $derived(
		selectedEntries.reduce((n, e) => n + (isDirLike(e) ? 0 : e.size), 0)
	);

	const mods = (e: MouseEvent) => ({ shift: e.shiftKey, meta: e.metaKey || e.ctrlKey });

	function onRowClick(e: MouseEvent, index: number) {
		sel = click(sel, visible, index, mods(e));
	}

	function onToggle(index: number, e: MouseEvent) {
		sel = toggle(sel, visible, index, e.shiftKey);
	}

	function onContextMenu(e: MouseEvent, index: number) {
		const entry = visible[index];
		if (!entry) {
			return;
		}
		e.preventDefault();
		if (!sel.selected.has(entry.path)) {
			sel = { selected: new Set([entry.path]), anchor: index, cursor: index };
		} else {
			sel = { ...sel, cursor: index };
		}
		ctx = { x: e.clientX, y: e.clientY, entry };
		ctxOpen = true;
	}

	function clearSelection() {
		sel = { ...sel, selected: new Set(), anchor: null };
	}

	// ---- actions ----

	function download(items: FileEntry[]) {
		if (!items.length) {
			return;
		}
		if (items.length === 1 && !isDirLike(items[0])) {
			return startDownload(rawUrl(deviceId, items[0].path, { download: true }));
		}
		const from = dirname(items[0].path);
		if (items.some((i) => dirname(i.path) !== from)) {
			return toast('Pick items from one folder to download them together', 'error');
		}
		filesApi
			.downloadArchive(
				deviceId,
				from,
				items.map((i) => i.name)
			)
			.catch((err) => toast(errorOf(err).message, 'error'));
	}

	function toggleBookmark(path: string) {
		const b = bookmarkPref.value;
		bookmarkPref.value = b.includes(path) ? b.filter((x) => x !== path) : [...b, path];
	}

	async function copyPath(path: string) {
		try {
			await navigator.clipboard.writeText(path);
			toast('Path copied');
		} catch {
			toast('Couldn’t copy. Select the path and copy it yourself.', 'error');
		}
	}

	async function duplicate(e: FileEntry) {
		try {
			const r = await filesApi.copy(deviceId, [e.path], dirname(e.path), 'rename');
			focusPath = r.paths[0] ?? null;
			toast(`Duplicated as ${basename(r.paths[0] ?? e.name)}`);
			reload();
		} catch (err) {
			toast(errorOf(err).message, 'error');
		}
	}

	function report(
		verb: string,
		r: { done: number; failed: Array<{ path: string; error: string }> }
	) {
		const m = bulkMessage(verb, r);
		toast(m.text, m.error ? 'error' : undefined);
	}

	// ---- uploads ----

	/** Makes each folder along a relative path, for the empty folders in a drop. */
	async function mkdirs(target: string, rel: string) {
		let at = target;
		for (const part of rel.split('/').filter(Boolean)) {
			await filesApi.mkdir(deviceId, at, part).catch((err) => {
				if (errorOf(err).status !== 409) {
					throw err;
				}
			});
			at = joinPath(at, part);
		}
	}

	async function upload(picked: Picked[], emptyDirs: string[] = []) {
		const target = dir;
		if (!target || (!picked.length && !emptyDirs.length)) {
			return;
		}
		for (const d of emptyDirs) {
			await mkdirs(target, d).catch((err) => toast(errorOf(err).message, 'error'));
		}
		if (!picked.length) {
			return reload();
		}
		let existing: string[] = [];
		try {
			existing = (
				await filesApi.exists(
					deviceId,
					target,
					picked.map((p) => p.name)
				)
			).existing;
		} catch (err) {
			return toast(errorOf(err).message, 'error');
		}
		const run = (mode: ConflictMode | 'skip') => {
			const clash = new Set(existing);
			const items = picked
				.filter((p) => !(mode === 'skip' && clash.has(p.name)))
				.map((p) => ({
					file: p.file,
					name: p.name,
					conflict: clash.has(p.name) && mode !== 'skip' ? mode : ('fail' as ConflictMode)
				}));
			if (items.length) {
				uploads.enqueue(deviceId, target, items);
			} else {
				toast('Nothing new to upload');
			}
		};
		if (existing.length) {
			ask = { existing, total: picked.length, run };
			askOpen = true;
		} else {
			run('fail');
		}
	}

	const hasFiles = (e: DragEvent) => e.dataTransfer?.types.includes('Files') ?? false;

	function onDragEnter(e: DragEvent) {
		if (!hasFiles(e) || !dir) {
			return;
		}
		e.preventDefault();
		dragDepth++;
		dragging = true;
	}

	function onDragOver(e: DragEvent) {
		if (!hasFiles(e) || !dir) {
			return;
		}
		e.preventDefault();
		e.dataTransfer!.dropEffect = 'copy';
	}

	function onDragLeave() {
		dragDepth = Math.max(0, dragDepth - 1);
		if (!dragDepth) {
			dragging = false;
		}
	}

	function onDrop(e: DragEvent) {
		if (!hasFiles(e)) {
			return;
		}
		e.preventDefault();
		dragDepth = 0;
		dragging = false;
		// The drop's entries are only there during this event, so start reading them now.
		fromDrop(e.dataTransfer!)
			.then(({ files: f, emptyDirs }) => upload(f, emptyDirs))
			.catch((err: Error) => toast(`Couldn’t read the dropped files: ${err.message}`, 'error'));
	}

	// Pasting files, say a screenshot, uploads them to the folder.
	function onPaste(e: ClipboardEvent) {
		if (filePath || dialog || !e.clipboardData?.files.length) {
			return;
		}
		if ((e.target as HTMLElement | null)?.closest('input, textarea, [contenteditable="true"]')) {
			return;
		}
		e.preventDefault();
		void upload(fromInput(e.clipboardData.files));
	}

	// ---- search ----

	async function runSearch(query: string) {
		if (!dir || !query.trim()) {
			return;
		}
		const root = dir;
		search = { root, query, result: null, error: null };
		sel = emptySelection();
		try {
			const result = await filesApi.search(deviceId, root, query.trim());
			if (search?.root === root && search.query === query) {
				search = { ...search, result };
			}
		} catch (err) {
			if (search?.root === root && search.query === query) {
				search = { ...search, error: errorOf(err).message };
			}
		}
	}

	function closeSearch() {
		search = null;
		searchOpen = false;
		searchText = '';
		sel = emptySelection();
	}

	function openSearch() {
		searchOpen = true;
		setTimeout(() => searchInput?.focus(), 0);
	}

	// ---- keyboard ----

	function onKey(e: KeyboardEvent) {
		if (filePath || dialog || askOpen || ctxOpen || e.defaultPrevented) {
			return;
		}
		const t = e.target as HTMLElement | null;
		if (
			t?.closest('input, textarea, select, [contenteditable="true"], [role=menu], [role=dialog]')
		) {
			return;
		}
		const mod = e.metaKey || e.ctrlKey;
		const at = visible[sel.cursor];
		if (e.key === 'ArrowDown') {
			sel = move(sel, visible, sel.cursor + 1, e.shiftKey);
		} else if (e.key === 'ArrowUp' && e.altKey) {
			goUp();
		} else if (e.key === 'ArrowUp') {
			sel = move(sel, visible, sel.cursor < 0 ? 0 : sel.cursor - 1, e.shiftKey);
		} else if (e.key === 'Home') {
			sel = move(sel, visible, 0, e.shiftKey);
		} else if (e.key === 'End') {
			sel = move(sel, visible, visible.length - 1, e.shiftKey);
		} else if (e.key === 'PageDown') {
			sel = move(sel, visible, sel.cursor + 15, e.shiftKey);
		} else if (e.key === 'PageUp') {
			sel = move(sel, visible, sel.cursor - 15, e.shiftKey);
		} else if (e.key === 'Enter' && at) {
			open(at);
		} else if (e.key === ' ' && at) {
			sel = toggleCursor(sel, visible);
		} else if (e.key === 'Backspace' && !mod) {
			if (search) {
				closeSearch();
			} else {
				goUp();
			}
		} else if (
			(e.key === 'Delete' || (e.key === 'Backspace' && mod)) &&
			(selectedEntries.length || at)
		) {
			dialog = { kind: 'delete', items: selectedEntries.length ? selectedEntries : [at] };
		} else if (
			e.key === 'F2' &&
			(selectedEntries.length === 1 || (!selectedEntries.length && at))
		) {
			dialog = { kind: 'rename', entry: selectedEntries[0] ?? at };
		} else if (mod && e.key.toLowerCase() === 'a') {
			sel = { ...sel, selected: new Set(visible.map((x) => x.path)) };
		} else if (e.key === 'Escape') {
			if (sel.selected.size) {
				clearSelection();
			} else if (search || searchOpen) {
				closeSearch();
			} else {
				return;
			}
		} else if (e.key === '/' || (mod && e.key.toLowerCase() === 'f' && e.shiftKey)) {
			openSearch();
		} else {
			return;
		}
		e.preventDefault();
	}

	// ---- dialogs ----

	// Each dialog opens when `dialog` is its kind, and closes by clearing it.
	const dialogOpen = (kind: DialogState['kind'] | DialogState['kind'][]) => {
		const kinds = Array.isArray(kind) ? kind : [kind];
		return {
			get: () => dialog !== null && kinds.includes(dialog.kind),
			set: (open: boolean) => {
				if (!open) {
					dialog = null;
				}
			}
		};
	};
	const mkdirOpen = dialogOpen('mkdir');
	const newfileOpen = dialogOpen('newfile');
	const renameOpen = dialogOpen('rename');
	const destOpen = dialogOpen(['move', 'copy']);
	const deleteOpen = dialogOpen('delete');

	// ---- view ----

	const places = $derived(quickPlaces(os, home));
	const bookmarked = $derived(dir !== null && bookmarkPref.value.includes(dir));
	const loadingFirst = $derived(!current && !error);
	const stats = $derived(
		totals(
			search?.result
				? visible
				: (current?.entries ?? []).filter((e) => showHidden.value || !isHidden(e))
		)
	);
	const searching = $derived(search !== null && !search.result && !search.error);
	const terminalPath = $derived(resolve('/terminal'));

	function onSort(key: SortKey) {
		sortPref.value = nextSort(sortPref.value, key);
	}
</script>

<svelte:window onkeydown={onKey} onpaste={onPaste} />

{#snippet placesList(onpick: (path: string) => void)}
	<Places
		{places}
		bookmarks={bookmarkPref.value}
		current={dir}
		{home}
		space={current?.space}
		ongo={onpick}
		onremovebookmark={(p) => (bookmarkPref.value = bookmarkPref.value.filter((x) => x !== p))}
	/>
{/snippet}

<!-- The items for one entry, in its actions menu and in the right-click menu. -->
{#snippet rowMenu(e: FileEntry)}
	{@const folder = isDirLike(e)}
	<MenuItem icon={folder ? FolderOpen : Eye} onSelect={() => open(e)}>
		{folder ? 'Open' : 'Preview'}
	</MenuItem>
	<MenuItem icon={Download} onSelect={() => download([e])}>
		{folder ? 'Download as .tar.gz' : 'Download'}
	</MenuItem>
	{#if folder}
		<MenuItem icon={SquareTerminal} href={terminalHref(terminalPath, deviceId, e.path)}>
			Open in terminal
		</MenuItem>
	{/if}
	<MenuSeparator />
	<MenuItem icon={Pencil} onSelect={() => (dialog = { kind: 'rename', entry: e })}>Rename</MenuItem>
	<MenuItem icon={CopyPlus} onSelect={() => duplicate(e)}>Duplicate</MenuItem>
	<MenuItem icon={FolderInput} onSelect={() => (dialog = { kind: 'move', items: [e] })}>
		Move to…
	</MenuItem>
	<MenuItem icon={Copy} onSelect={() => (dialog = { kind: 'copy', items: [e] })}>Copy to…</MenuItem>
	<MenuItem icon={ClipboardCopy} onSelect={() => copyPath(e.path)}>Copy path</MenuItem>
	{#if folder}
		<MenuItem icon={Star} onSelect={() => toggleBookmark(e.path)}>
			{bookmarkPref.value.includes(e.path) ? 'Remove bookmark' : 'Bookmark'}
		</MenuItem>
	{/if}
	<MenuSeparator />
	<MenuItem danger icon={Trash2} onSelect={() => (dialog = { kind: 'delete', items: [e] })}>
		Delete
	</MenuItem>
{/snippet}

{#snippet bulkMenu(items: FileEntry[])}
	<MenuLabel>{items.length} items</MenuLabel>
	<MenuItem icon={Download} onSelect={() => download(items)}>Download as .tar.gz</MenuItem>
	<MenuItem icon={FolderInput} onSelect={() => (dialog = { kind: 'move', items })}
		>Move to…</MenuItem
	>
	<MenuItem icon={Copy} onSelect={() => (dialog = { kind: 'copy', items })}>Copy to…</MenuItem>
	<MenuSeparator />
	<MenuItem danger icon={Trash2} onSelect={() => (dialog = { kind: 'delete', items })}>
		Delete
	</MenuItem>
{/snippet}

{#snippet emptyView()}
	{#if search}
		{#if search.error}
			<EmptyState icon={AlertTriangle} title="Search failed">{search.error}</EmptyState>
		{:else}
			<EmptyState icon={Search} title="Nothing named like “{search.query}”">
				Searched {tildify(search.root, home)} and its subfolders{search.result?.truncated
					? ' (stopped early)'
					: ''}. .git and node_modules are skipped.
			</EmptyState>
		{/if}
	{:else if hiddenCount && !showHidden.value}
		<EmptyState icon={EyeOff} title="Only hidden files here">
			<Button size="sm" class="mt-2" onclick={() => (showHidden.value = true)}>
				Show {plural(hiddenCount, 'hidden item')}
			</Button>
		</EmptyState>
	{:else}
		<EmptyState icon={FolderOpen} title="This folder is empty">
			Drop files here, paste them, or use Upload.
		</EmptyState>
	{/if}
{/snippet}

<div class="grid gap-4 lg:grid-cols-[200px_minmax(0,1fr)]">
	<aside class="hidden lg:block">
		<div class="sticky top-20">{@render placesList(go)}</div>
	</aside>

	<section
		class="relative min-w-0 self-start card"
		ondragenter={onDragEnter}
		ondragover={onDragOver}
		ondragleave={onDragLeave}
		ondrop={onDrop}
		aria-label="File browser"
	>
		<div class="flex flex-wrap items-center gap-1.5 rounded-t-xl border-b border-line p-2">
			<div class="flex items-center">
				<div class="lg:hidden">
					<Menu bind:open={placesOpen} align="start">
						{#snippet trigger({ props })}
							<IconButton label="Places" {...props}><MapPin class="size-4" /></IconButton>
						{/snippet}
						<div class="w-60 p-1">
							{@render placesList((p) => {
								placesOpen = false;
								go(p);
							})}
						</div>
					</Menu>
				</div>
				<IconButton label="Up a folder (Backspace)" onclick={goUp} disabled={!parent}>
					<ArrowUp class="size-4" />
				</IconButton>
				<IconButton label="Refresh" onclick={reload}>
					<RefreshCw class={['size-4', refreshing && current && 'animate-spin']} />
				</IconButton>
			</div>
			<PathBar
				path={current?.path ?? (showError ? pathParam : null)}
				{home}
				ongo={(p) => void goTyped(p)}
				class="order-last w-full sm:order-none sm:w-auto sm:flex-1"
			/>
			<div class="ml-auto flex items-center gap-0.5 sm:ml-0">
				<IconButton
					label="Search names (/)"
					onclick={() => (searchOpen ? closeSearch() : openSearch())}
					class={[searchOpen && 'bg-hover text-fg']}
				>
					<Search class="size-4" />
				</IconButton>
				<IconButton
					label={bookmarked ? 'Remove bookmark' : 'Bookmark this folder'}
					onclick={() => dir && toggleBookmark(dir)}
					disabled={!dir}
					class="max-sm:hidden"
				>
					<Star class={['size-4', bookmarked && 'fill-amber text-amber']} />
				</IconButton>
				<IconButton
					label={showHidden.value ? 'Hide hidden files' : 'Show hidden files'}
					onclick={() => (showHidden.value = !showHidden.value)}
					class={[showHidden.value && 'bg-hover text-fg']}
				>
					{#if showHidden.value}<Eye class="size-4" />{:else}<EyeOff class="size-4" />{/if}
				</IconButton>
				{#if dir}
					<a
						href={terminalHref(terminalPath, deviceId, dir)}
						title="Open a terminal here"
						aria-label="Open a terminal here"
						class="hidden size-8 items-center justify-center rounded-md text-fg-subtle transition-colors hover:bg-hover hover:text-fg sm:inline-flex"
					>
						<SquareTerminal class="size-4" />
					</a>
				{/if}
				<span class="mx-1 hidden h-5 w-px bg-line-strong sm:block"></span>
				<Menu>
					{#snippet trigger({ props })}
						<Button size="sm" disabled={!dir} {...props} class="px-2 sm:px-3">
							{#snippet icon()}<Plus class="size-3.5" />{/snippet}
							<span class="hidden sm:inline">New</span>
						</Button>
					{/snippet}
					<MenuItem icon={FolderPlus} onSelect={() => (dialog = { kind: 'mkdir' })}
						>New folder</MenuItem
					>
					<MenuItem icon={FilePlus} onSelect={() => (dialog = { kind: 'newfile' })}>
						New text file
					</MenuItem>
				</Menu>
				<Menu>
					{#snippet trigger({ props })}
						<Button size="sm" variant="primary" disabled={!dir} {...props} class="px-2 sm:px-3">
							{#snippet icon()}<Upload class="size-3.5" />{/snippet}
							<span class="hidden sm:inline">Upload</span>
						</Button>
					{/snippet}
					<MenuItem icon={Upload} onSelect={() => fileInput?.click()}>Upload files</MenuItem>
					<MenuItem icon={FolderUp} onSelect={() => dirInput?.click()}>Upload a folder</MenuItem>
					<MenuLabel>Or drop files and folders onto the list.</MenuLabel>
				</Menu>
				<Menu>
					{#snippet trigger({ props })}
						<IconButton label="More" {...props}><Ellipsis class="size-4" /></IconButton>
					{/snippet}
					<MenuItem
						icon={Download}
						onSelect={() => {
							if (dir === '/') {
								toast('Select folders inside / to download them', 'error');
							} else if (dir) {
								filesApi
									.downloadArchive(deviceId, dirname(dir), [basename(dir)])
									.catch((err) => toast(errorOf(err).message, 'error'));
							}
						}}
					>
						Download this folder
					</MenuItem>
					<MenuItem icon={ClipboardCopy} onSelect={() => dir && copyPath(dir)}>Copy path</MenuItem>
					<MenuItem icon={Star} onSelect={() => dir && toggleBookmark(dir)}>
						{bookmarked ? 'Remove bookmark' : 'Bookmark this folder'}
					</MenuItem>
					{#if dir}
						<MenuItem icon={SquareTerminal} href={terminalHref(terminalPath, deviceId, dir)}>
							Open terminal here
						</MenuItem>
					{/if}
				</Menu>
			</div>
		</div>

		{#if searchOpen}
			<form
				class="flex items-center gap-2 border-b border-line px-3 py-2"
				onsubmit={(e) => {
					e.preventDefault();
					void runSearch(searchText);
				}}
			>
				<Search class="size-3.5 shrink-0 text-fg-faint" />
				<input
					bind:this={searchInput}
					bind:value={searchText}
					onkeydown={(e) => {
						if (e.key === 'Escape') {
							e.preventDefault();
							closeSearch();
						}
					}}
					placeholder="Search names in {dir ? tildify(dir, home) : 'this folder'} and below…"
					spellcheck="false"
					aria-label="Search names"
					class="h-7 min-w-0 flex-1 bg-transparent text-[13px] text-fg placeholder:text-fg-faint focus:outline-none focus-visible:shadow-none"
				/>
				{#if searching}
					<span class="size-3.5 animate-spin rounded-full border-2 border-fg-faint border-t-fg"
					></span>
				{/if}
				<Button size="xs" type="submit" disabled={!searchText.trim()}>Search</Button>
				<IconButton label="Close search" size="sm" onclick={closeSearch}>
					<X class="size-3.5" />
				</IconButton>
			</form>
		{/if}
		{#if search?.result}
			<div
				class="flex items-center gap-2 border-b border-line bg-raised/60 px-3 py-1.5 text-[12px] text-fg-subtle"
			>
				<span class="min-w-0 flex-1 truncate">
					{plural(search.result.entries.length, 'match', 'matches')} for “<span class="text-fg"
						>{search.query}</span
					>” in {tildify(search.root, home)}
					{#if search.result.truncated}· showing the first ones{/if}
				</span>
				<button type="button" onclick={closeSearch} class="shrink-0 text-fg-muted hover:text-fg">
					Back to folder
				</button>
			</div>
		{/if}

		{#if showError && error}
			{@const denied = error.status === 403}
			{@const missing = error.status === 404}
			<EmptyState
				icon={denied ? Lock : AlertTriangle}
				title={denied
					? 'No permission to open this folder'
					: missing
						? 'This folder doesn’t exist'
						: 'Couldn’t open this folder'}
			>
				<p class="font-mono text-[12px] break-all text-fg-subtle">{error.message}</p>
				<div class="mt-3 flex justify-center gap-2">
					{#if parent}
						<Button size="sm" onclick={() => go(parent)}>
							{#snippet icon()}<ArrowUp class="size-3.5" />{/snippet}
							Go up
						</Button>
					{/if}
					<Button size="sm" variant="ghost" onclick={() => go(home ?? '~')}>Home</Button>
					{#if !denied && !missing}
						<Button size="sm" variant="ghost" onclick={reload}>
							{#snippet icon()}<RefreshCw class="size-3.5" />{/snippet}
							Try again
						</Button>
					{/if}
				</div>
			</EmptyState>
		{:else if loadingFirst || searching}
			<div>
				{#each { length: 9 }, i (i)}
					<div class="flex h-9 items-center gap-3 border-b border-line px-3 last:border-0">
						<Skeleton class="size-4" />
						<Skeleton class="h-3 {['w-40', 'w-56', 'w-32', 'w-64', 'w-48'][i % 5]}" />
						<Skeleton class="ml-auto hidden h-3 w-16 sm:block" />
						<Skeleton class="hidden h-3 w-20 sm:block" />
					</div>
				{/each}
			</div>
		{:else}
			<Listing
				entries={visible}
				selected={sel.selected}
				cursor={sel.cursor}
				sort={sortPref.value}
				{now}
				searchRoot={search?.result ? search.root : null}
				{home}
				onsort={onSort}
				onrowclick={onRowClick}
				ontoggle={onToggle}
				ontoggleall={() => (sel = toggleAll(sel, visible))}
				onopen={open}
				oncontextmenu={onContextMenu}
				menu={rowMenu}
				empty={emptyView}
			/>
		{/if}

		{#if current && !showError}
			<div
				class="flex items-center gap-3 rounded-b-xl border-t border-line px-3 py-2 num text-[11.5px] text-fg-faint"
			>
				<span>
					{plural(stats.folders, 'folder')} · {plural(stats.files, 'file')} · {bytes(stats.size)}
				</span>
				{#if !search && hiddenCount > 0 && !showHidden.value}
					<button
						type="button"
						onclick={() => (showHidden.value = true)}
						class="hover:text-fg-muted pointer-coarse:-my-2 pointer-coarse:py-2"
					>
						{hiddenCount} hidden
					</button>
				{/if}
				<span class="ml-auto hidden sm:inline">Drop files to upload · ↑↓ Enter Backspace</span>
			</div>
		{/if}

		{#if dragging}
			<div
				class="pointer-events-none absolute inset-0 z-20 flex items-center justify-center rounded-xl border-2 border-dashed border-blue-fg bg-blue/[0.07] backdrop-blur-[1px]"
			>
				<div
					class="rounded-lg border border-line-strong bg-surface px-4 py-3 text-center shadow-2xl"
				>
					<Upload class="mx-auto mb-1.5 size-5 text-blue-fg" />
					<div class="text-[13px] font-medium text-fg">Drop to upload</div>
					<div class="font-mono text-[11.5px] text-fg-subtle">{dir ? tildify(dir, home) : ''}</div>
				</div>
			</div>
		{/if}
	</section>

	<input
		bind:this={fileInput}
		type="file"
		multiple
		hidden
		onchange={(e) => {
			void upload(fromInput(e.currentTarget.files));
			e.currentTarget.value = '';
		}}
	/>
	<input
		bind:this={dirInput}
		type="file"
		multiple
		hidden
		webkitdirectory
		onchange={(e) => {
			void upload(fromInput(e.currentTarget.files));
			e.currentTarget.value = '';
		}}
	/>

	{#if selectedEntries.length > 0 && !filePath}
		<div
			class="fixed inset-x-2 bottom-2 z-30 flex items-center gap-1 rounded-xl border border-line-strong bg-surface p-1.5 pl-3 shadow-popover sm:inset-x-auto sm:bottom-5 sm:left-1/2 sm:-translate-x-1/2 lg:ml-[120px]"
		>
			<span class="mr-1 shrink-0 num text-[12.5px] text-fg">
				{selectedEntries.length} selected
				{#if selectedSize > 0}
					<span class="hidden text-fg-subtle sm:inline">· {bytes(selectedSize)}</span>
				{/if}
			</span>
			<span class="mx-1 h-5 w-px shrink-0 bg-line-strong"></span>
			{#snippet bulk(label: string, onclick: () => void, danger = false)}
				<button
					type="button"
					{onclick}
					title={label}
					class={[
						'flex h-8 shrink-0 items-center gap-1.5 rounded-md px-2.5 text-[12.5px] font-medium transition-colors',
						danger ? 'text-red-fg hover:bg-red/10' : 'text-fg-muted hover:bg-hover hover:text-fg'
					]}
				>
					{#if label.startsWith('Download')}
						<Download class="size-3.5" />
					{:else if label === 'Move'}
						<FolderInput class="size-3.5" />
					{:else if label === 'Copy'}
						<Copy class="size-3.5" />
					{:else}
						<Trash2 class="size-3.5" />
					{/if}
					<span class="hidden sm:inline">{label}</span>
				</button>
			{/snippet}
			{@render bulk(
				selectedEntries.length === 1 && !isDirLike(selectedEntries[0])
					? 'Download'
					: 'Download .tar.gz',
				() => download(selectedEntries)
			)}
			{@render bulk('Move', () => (dialog = { kind: 'move', items: selectedEntries }))}
			{@render bulk('Copy', () => (dialog = { kind: 'copy', items: selectedEntries }))}
			{@render bulk('Delete', () => (dialog = { kind: 'delete', items: selectedEntries }), true)}
			<IconButton
				label="Clear selection (Esc)"
				size="sm"
				class="ml-auto shrink-0 sm:ml-1"
				onclick={clearSelection}
			>
				<X class="size-3.5" />
			</IconButton>
		</div>
	{/if}

	{#if ctx}
		<ContextMenu bind:open={ctxOpen} x={ctx.x} y={ctx.y}>
			{#if selectedEntries.length > 1 && sel.selected.has(ctx.entry.path)}
				{@render bulkMenu(selectedEntries)}
			{:else}
				{@render rowMenu(ctx.entry)}
			{/if}
		</ContextMenu>
	{/if}

	{#if previewEntry}
		<Preview
			device={deviceId}
			entry={previewEntry}
			{siblings}
			{home}
			onnavigate={(e) =>
				void goto(filesUrl(deviceId, pathParam, e.path), { replace: true, reset: false })}
			onclose={closePreview}
			onsaved={reload}
		/>
	{/if}

	<NameDialog
		bind:open={mkdirOpen.get, mkdirOpen.set}
		title="New folder"
		description={dir ? `In ${tildify(dir, home)}` : undefined}
		initial="New folder"
		confirm="Create"
		onsubmit={async (name) => {
			if (!dir) {
				return;
			}
			const e = await filesApi.mkdir(deviceId, dir, name);
			focusPath = e.path;
			toast(`Created ${name}`);
			reload();
		}}
	/>
	<NameDialog
		bind:open={newfileOpen.get, newfileOpen.set}
		title="New text file"
		description={dir ? `In ${tildify(dir, home)}` : undefined}
		initial="untitled.txt"
		confirm="Create"
		onsubmit={async (name) => {
			if (!dir) {
				return;
			}
			const e = await filesApi.write(deviceId, joinPath(dir, name), '', { create: true });
			focusPath = e.path;
			reload();
			openPreview(e);
		}}
	/>
	<NameDialog
		bind:open={renameOpen.get, renameOpen.set}
		title="Rename"
		initial={dialog?.kind === 'rename' ? dialog.entry.name : ''}
		confirm="Rename"
		onsubmit={async (name) => {
			if (dialog?.kind !== 'rename') {
				return;
			}
			const e = await filesApi.rename(deviceId, dialog.entry.path, name);
			focusPath = e.path;
			toast(`Renamed to ${name}`);
			reload();
		}}
	/>
	<DestinationDialog
		bind:open={destOpen.get, destOpen.set}
		mode={dialog?.kind === 'copy' ? 'copy' : 'move'}
		device={deviceId}
		items={dialog?.kind === 'move' || dialog?.kind === 'copy' ? dialog.items : []}
		start={dir ?? home ?? '/'}
		{home}
		onsubmit={async (dest, conflict) => {
			if (dialog?.kind !== 'move' && dialog?.kind !== 'copy') {
				return;
			}
			let items = dialog.items;
			if (conflict === 'skip') {
				const names = new Set((await filesApi.list(deviceId, dest)).entries.map((e) => e.name));
				items = items.filter((i) => !names.has(i.name));
			}
			const paths = items.map((i) => i.path);
			const mode = conflict === 'skip' ? 'fail' : conflict;
			const r =
				dialog.kind === 'move'
					? await filesApi.move(deviceId, paths, dest, mode)
					: await filesApi.copy(deviceId, paths, dest, mode);
			report(dialog.kind === 'move' ? 'Moved' : 'Copied', r);
			clearSelection();
			reload();
		}}
	/>
	<DeleteDialog
		bind:open={deleteOpen.get, deleteOpen.set}
		items={dialog?.kind === 'delete' ? dialog.items : []}
		onconfirm={async () => {
			if (dialog?.kind !== 'delete') {
				return;
			}
			const gone = dialog.items;
			const r = await filesApi.remove(
				deviceId,
				gone.map((i) => i.path)
			);
			report('Deleted', r);
			clearSelection();
			if (search?.result) {
				const entries = search.result.entries.filter((e) => !gone.some((i) => i.path === e.path));
				search = { ...search, result: { ...search.result, entries } };
			}
			reload();
		}}
	/>
	<UploadConflictDialog
		bind:open={askOpen}
		existing={ask?.existing ?? []}
		total={ask?.total ?? 0}
		onchoose={(mode) => {
			ask?.run(mode);
			askOpen = false;
		}}
	/>
	<UploadPanel />
</div>
