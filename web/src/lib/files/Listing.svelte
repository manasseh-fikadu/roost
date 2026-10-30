<!--
	The files of a folder as a table that scrolls with the page: sortable
	columns, a checkbox on each row, and only the rows on screen in the DOM.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import ArrowDown from '@lucide/svelte/icons/arrow-down';
	import ArrowUp from '@lucide/svelte/icons/arrow-up';
	import Check from '@lucide/svelte/icons/check';
	import Minus from '@lucide/svelte/icons/minus';
	import type { FileEntry } from '#lib/api/types.js';
	import ListingRow from './ListingRow.svelte';
	import { ROW_DESKTOP, ROW_MOBILE, gridClass, scrollToRow, visibleRange } from './listing.js';
	import { tildify } from './paths.js';
	import type { Sort, SortKey } from './sort.js';

	interface Props {
		entries: FileEntry[];
		selected: Set<string>;
		/** The row the keyboard is on, or -1. */
		cursor: number;
		sort: Sort;
		now: number;
		/** The folder a search started in, if the entries are its matches. */
		searchRoot: string | null;
		home: string | null;
		onsort: (key: SortKey) => void;
		onrowclick: (e: MouseEvent, index: number) => void;
		ontoggle: (index: number, e: MouseEvent) => void;
		ontoggleall: () => void;
		onopen: (entry: FileEntry) => void;
		oncontextmenu: (e: MouseEvent, index: number) => void;
		/** The items of a row's actions menu. */
		menu: Snippet<[FileEntry]>;
		/** What to show with no entries. */
		empty: Snippet;
	}

	let {
		entries,
		selected,
		cursor,
		sort,
		now,
		searchRoot,
		home,
		onsort,
		onrowclick,
		ontoggle,
		ontoggleall,
		onopen,
		oncontextmenu,
		menu,
		empty
	}: Props = $props();

	let body = $state<HTMLDivElement>();
	let rowH = $state(ROW_DESKTOP);
	let range = $state({ start: 0, end: 60 });

	/** Works out which rows are on screen, given that the list scrolls with the page. */
	function update() {
		if (!body) {
			return;
		}
		const h = matchMedia('(min-width: 640px)').matches ? ROW_DESKTOP : ROW_MOBILE;
		const next = visibleRange(body.getBoundingClientRect().top, innerHeight, h, entries.length);
		rowH = h;
		if (next.start !== range.start || next.end !== range.end) {
			range = next;
		}
	}

	$effect(() => {
		void [entries.length, body];
		update();
	});

	// Keyboard moves, and a jump to a renamed item, scroll the cursor's row into view.
	$effect(() => {
		if (cursor < 0 || !body) {
			return;
		}
		const by = scrollToRow(body.getBoundingClientRect().top, cursor, rowH, innerHeight);
		if (by) {
			scrollBy({ top: by });
		}
	});

	const all = $derived(entries.length > 0 && entries.every((e) => selected.has(e.path)));
	const some = $derived(!all && entries.some((e) => selected.has(e.path)));

	/** Where a search match is, relative to where the search started. */
	function folderOf(p: string): string | null {
		if (!searchRoot) {
			return null;
		}
		const dir = p.slice(0, p.lastIndexOf('/')) || '/';
		if (dir === searchRoot) {
			return './';
		}
		return dir.startsWith(`${searchRoot}/`)
			? `./${dir.slice(searchRoot.length + 1)}`
			: tildify(dir, home);
	}

	const heads: Array<{ key: SortKey; label: string; class: string }> = [
		{ key: 'name', label: 'Name', class: '' },
		{ key: 'size', label: 'Size', class: 'hidden justify-end pr-3 sm:flex' },
		{ key: 'mtime', label: 'Modified', class: 'hidden sm:flex' },
		{ key: 'mode', label: 'Permissions', class: 'hidden lg:flex' },
		{ key: 'owner', label: 'Owner', class: 'hidden lg:flex' }
	];
</script>

<svelte:window onscroll={update} onresize={update} />

<div role="grid" aria-multiselectable="true" aria-rowcount={entries.length}>
	<div
		role="row"
		class={[
			gridClass,
			'sticky top-14 z-[5] h-9 items-center border-b border-line bg-surface/95 backdrop-blur'
		]}
	>
		<div role="columnheader" class="h-full">
			{#if entries.length > 0}
				<button
					type="button"
					role="checkbox"
					aria-checked={all ? true : some ? 'mixed' : false}
					aria-label="Select all"
					onclick={ontoggleall}
					class="flex size-full items-center justify-center"
				>
					<span
						class={[
							'flex size-4 items-center justify-center rounded border transition-colors',
							all || some ? 'border-blue-fg bg-blue text-white' : 'border-line-heavy bg-canvas'
						]}
					>
						{#if all}<Check class="size-3" strokeWidth={3} />{/if}
						{#if some}<Minus class="size-3" strokeWidth={3} />{/if}
					</span>
				</button>
			{/if}
		</div>
		{#each heads as h (h.key)}
			{@const active = sort.key === h.key}
			<button
				type="button"
				role="columnheader"
				onclick={() => onsort(h.key)}
				class={[
					'flex h-full min-w-0 items-center gap-1 text-left text-[12px] transition-colors',
					active ? 'text-fg' : 'text-fg-subtle hover:text-fg',
					h.class
				]}
			>
				<span class="truncate">{h.label}</span>
				{#if active}
					{#if sort.dir === 1}
						<ArrowUp class="size-3 shrink-0" />
					{:else}
						<ArrowDown class="size-3 shrink-0" />
					{/if}
				{/if}
			</button>
		{/each}
		<div></div>
	</div>
	{#if entries.length === 0}
		{@render empty()}
	{:else}
		<div
			bind:this={body}
			class="-mb-px"
			style:padding-top="{range.start * rowH}px"
			style:padding-bottom="{Math.max(0, entries.length - range.end) * rowH}px"
		>
			{#each entries.slice(range.start, range.end) as entry, k (entry.path)}
				{@const i = range.start + k}
				<ListingRow
					{entry}
					index={i}
					selected={selected.has(entry.path)}
					cursor={i === cursor}
					{now}
					folder={folderOf(entry.path)}
					{onrowclick}
					{ontoggle}
					{onopen}
					{oncontextmenu}
					{menu}
				/>
			{/each}
		</div>
	{/if}
</div>
