<!--
	A file opened over the whole window: images, video, PDFs, text with an
	editor, Word documents and spreadsheets. Arrow keys move between the
	folder's files, and Escape closes it.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import ChevronLeft from '@lucide/svelte/icons/chevron-left';
	import ChevronRight from '@lucide/svelte/icons/chevron-right';
	import Download from '@lucide/svelte/icons/download';
	import ExternalLink from '@lucide/svelte/icons/external-link';
	import Info from '@lucide/svelte/icons/info';
	import X from '@lucide/svelte/icons/x';
	import type { FileEntry } from '#lib/api/types.js';
	import { ago, bytes, dateTime } from '#lib/format.js';
	import CopyButton from '#lib/ui/CopyButton.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import { mtimeOf, rawUrl, startDownload } from './api.js';
	import FileIcon from './FileIcon.svelte';
	import { canInline } from './kinds.js';
	import { dirname, tildify } from './paths.js';
	import Viewer from './Viewer.svelte';

	interface Props {
		device: string;
		entry: FileEntry;
		/** The folder's files, in the listing's order, to move between. */
		siblings: FileEntry[];
		home: string | null;
		onnavigate: (entry: FileEntry) => void;
		onclose: () => void;
		/** Called after the editor saved, so the listing can refresh. */
		onsaved: () => void;
	}

	let { device, entry, siblings, home, onnavigate, onclose, onsaved }: Props = $props();

	let info = $state(false);
	/** The entry as it is now, which saving changes until the folder is listed again. */
	let current = $derived<FileEntry>(entry);
	let dirty = false;

	const index = $derived(siblings.findIndex((s) => s.path === entry.path));
	const prev = $derived(index > 0 ? siblings[index - 1] : null);
	const next = $derived(index >= 0 && index < siblings.length - 1 ? siblings[index + 1] : null);
	const modified = $derived(mtimeOf(current));

	/** Runs fn, after asking if there are unsaved changes to lose. */
	function leave(fn: () => void) {
		if (dirty && !confirm('You have unsaved changes. Discard them?')) {
			return;
		}
		dirty = false;
		fn();
	}

	function onKey(e: KeyboardEvent) {
		if (e.defaultPrevented) {
			return;
		}
		const t = e.target as HTMLElement | null;
		const inField = t?.closest('input, textarea, select');
		if (e.key === 'Escape' && !inField) {
			leave(onclose);
		} else if (inField || t?.closest('[contenteditable="true"], .cm-editor')) {
			return;
		} else if (e.key === 'ArrowLeft' && prev) {
			leave(() => onnavigate(prev));
		} else if (e.key === 'ArrowRight' && next) {
			leave(() => onnavigate(next));
		} else {
			return;
		}
		e.preventDefault();
	}

	// The page behind doesn't scroll while the preview is open.
	$effect(() => {
		const overflow = document.body.style.overflow;
		document.body.style.overflow = 'hidden';
		return () => {
			document.body.style.overflow = overflow;
		};
	});
</script>

{#snippet row(label: string, children: Snippet, copy?: string)}
	<div class="border-b border-line px-4 py-2.5 last:border-0">
		<div class="text-[11.5px] text-fg-subtle">{label}</div>
		<div class="mt-0.5 flex items-start gap-1">
			<div class="min-w-0 flex-1 num text-[12.5px] break-all text-fg">{@render children()}</div>
			{#if copy}<span class="-mt-1 -mr-2"><CopyButton value={copy} /></span>{/if}
		</div>
	</div>
{/snippet}

<svelte:window onkeydown={onKey} />

<div
	class="fixed inset-0 z-50 flex flex-col bg-canvas"
	role="dialog"
	aria-modal="true"
	aria-label="Preview of {current.name}"
>
	<header
		class="flex h-12 shrink-0 items-center gap-1 border-b border-line bg-canvas pr-1.5 pl-3 sm:gap-2 sm:px-3"
	>
		<span
			class="hidden size-8 shrink-0 items-center justify-center rounded-md border border-line bg-raised sm:flex"
		>
			<FileIcon entry={current} />
		</span>
		<div class="min-w-0 flex-1">
			<div class="truncate text-[13.5px] font-medium text-fg" title={current.path}>
				{current.name}
			</div>
			<div class="truncate num text-[11px] text-fg-subtle">
				{bytes(current.size)} ·
				<span title={dateTime(modified)}>{ago(modified, Math.max(Date.now(), modified))}</span>
				<span class="hidden sm:inline">· {tildify(dirname(current.path), home)}</span>
			</div>
		</div>
		{#if siblings.length > 1 && index >= 0}
			<span class="hidden num text-[11.5px] text-fg-faint md:inline">
				{index + 1} / {siblings.length}
			</span>
		{/if}
		<div class="flex items-center">
			<IconButton
				label="Previous file (←)"
				disabled={!prev}
				onclick={() => prev && leave(() => onnavigate(prev))}
			>
				<ChevronLeft class="size-4" />
			</IconButton>
			<IconButton
				label="Next file (→)"
				disabled={!next}
				onclick={() => next && leave(() => onnavigate(next))}
			>
				<ChevronRight class="size-4" />
			</IconButton>
		</div>
		<span class="mx-0.5 hidden h-5 w-px bg-line-strong sm:block"></span>
		<IconButton
			label="File info"
			onclick={() => (info = !info)}
			class={[info && 'bg-hover text-fg']}
		>
			<Info class="size-4" />
		</IconButton>
		{#if canInline(current.name)}
			<a
				href={rawUrl(device, current.path)}
				target="_blank"
				rel="noopener noreferrer"
				aria-label="Open in a new tab"
				title="Open in a new tab"
				class="hidden size-8 items-center justify-center rounded-md text-fg-subtle transition-colors hover:bg-hover hover:text-fg sm:inline-flex"
			>
				<ExternalLink class="size-4" />
			</a>
		{/if}
		<IconButton
			label="Download"
			onclick={() => startDownload(rawUrl(device, current.path, { download: true }))}
		>
			<Download class="size-4" />
		</IconButton>
		<IconButton label="Close preview (Esc)" onclick={() => leave(onclose)}>
			<X class="size-4" />
		</IconButton>
	</header>
	<div class="relative flex min-h-0 flex-1">
		<main class="relative min-w-0 flex-1 bg-surface">
			{#key current.path}
				<Viewer
					{device}
					entry={current}
					ondirtychange={(d) => (dirty = d)}
					onsaved={(e) => {
						current = { ...current, size: e.size, modifiedAt: e.modifiedAt };
						onsaved();
					}}
				/>
			{/key}
		</main>
		{#if info}
			<aside
				class="absolute inset-y-0 right-0 z-10 w-72 max-w-[85vw] overflow-y-auto border-l border-line bg-canvas shadow-[-12px_0_32px_rgba(0,0,0,0.5)] md:static md:shadow-none"
			>
				<div class="flex h-10 items-center justify-between border-b border-line px-4">
					<span class="text-[13px] font-medium">Details</span>
					<IconButton label="Hide details" size="sm" onclick={() => (info = false)}>
						<X class="size-3.5" />
					</IconButton>
				</div>
				{#snippet path()}{current.path}{/snippet}
				{@render row('Path', path, current.path)}
				{#snippet size()}
					{bytes(current.size)}
					<span class="text-fg-faint">({current.size.toLocaleString()} bytes)</span>
				{/snippet}
				{@render row('Size', size)}
				{#snippet time()}
					{dateTime(modified)}
					<span class="text-fg-faint">· {ago(modified, Math.max(Date.now(), modified))}</span>
				{/snippet}
				{@render row('Modified', time)}
				{#snippet mode()}{current.mode}{/snippet}
				{@render row('Permissions', mode)}
				{#snippet owner()}{current.owner}:{current.group}{/snippet}
				{@render row('Owner', owner)}
				{#if current.type === 'symlink'}
					{#snippet target()}{current.target}{/snippet}
					{@render row('Link to', target)}
				{/if}
			</aside>
		{/if}
	</div>
</div>
