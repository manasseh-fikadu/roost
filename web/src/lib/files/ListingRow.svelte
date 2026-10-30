<!-- One row of the listing: a checkbox, the name, size, time, permissions, owner and an actions menu. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Check from '@lucide/svelte/icons/check';
	import Ellipsis from '@lucide/svelte/icons/ellipsis';
	import Minus from '@lucide/svelte/icons/minus';
	import type { FileEntry } from '#lib/api/types.js';
	import { ago, bytes, dateTime } from '#lib/format.js';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import FileIcon from './FileIcon.svelte';
	import { isDirLike } from './kinds.js';
	import { gridClass } from './listing.js';

	interface Props {
		entry: FileEntry;
		index: number;
		selected: boolean;
		/** The keyboard is on this row. */
		cursor: boolean;
		now: number;
		/** A search result shows which folder each match is in. */
		folder: string | null;
		onrowclick: (e: MouseEvent, index: number) => void;
		ontoggle: (index: number, e: MouseEvent) => void;
		onopen: (entry: FileEntry) => void;
		oncontextmenu: (e: MouseEvent, index: number) => void;
		menu: Snippet<[FileEntry]>;
	}

	let {
		entry,
		index,
		selected,
		cursor,
		now: tick,
		folder,
		onrowclick,
		ontoggle,
		onopen,
		oncontextmenu,
		menu
	}: Props = $props();

	const dir = $derived(isDirLike(entry));
	const modified = $derived(Date.parse(entry.modifiedAt));
	/** `now` ticks every 30 seconds, and the device's clock may run ahead: never say "in 2s". */
	const now = $derived(Math.max(tick, modified));
	const broken = $derived(entry.type === 'symlink' && !entry.targetType);
</script>

<!-- The row is a mouse target for selecting; the keyboard works through the listing's own keys. -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div
	role="row"
	tabindex="-1"
	aria-selected={selected}
	data-index={index}
	onclick={(e) => onrowclick(e, index)}
	ondblclick={() => onopen(entry)}
	oncontextmenu={(e) => oncontextmenu(e, index)}
	class={[
		gridClass,
		'group relative h-10 items-center border-b border-line text-[13px] select-none sm:h-9',
		selected ? 'bg-blue/[0.09]' : 'hover:bg-hover/50',
		cursor && 'shadow-[inset_2px_0_0_var(--color-blue-fg)]'
	]}
>
	<div
		role="gridcell"
		class={[
			'h-full transition-opacity',
			selected ? 'opacity-100' : 'opacity-60 group-hover:opacity-100'
		]}
	>
		<button
			type="button"
			role="checkbox"
			aria-checked={selected}
			aria-label="Select {entry.name}"
			onclick={(e) => {
				e.stopPropagation();
				ontoggle(index, e);
			}}
			ondblclick={(e) => e.stopPropagation()}
			class="flex size-full items-center justify-center"
		>
			<span
				class={[
					'flex size-4 items-center justify-center rounded border transition-colors',
					selected
						? 'border-blue-fg bg-blue text-white'
						: 'border-line-heavy bg-canvas group-hover:border-fg-faint'
				]}
			>
				{#if selected}<Check class="size-3" strokeWidth={3} />{/if}
			</span>
		</button>
	</div>
	<div role="gridcell" class="flex min-w-0 items-center gap-2.5 pr-3">
		<FileIcon {entry} />
		<div class="min-w-0">
			<div class="flex min-w-0 items-baseline gap-1.5">
				<button
					type="button"
					onclick={(e) => {
						// A click with a modifier selects, as on the rest of the row.
						if (e.metaKey || e.ctrlKey || e.shiftKey) {
							return;
						}
						e.stopPropagation();
						onopen(entry);
					}}
					ondblclick={(e) => e.stopPropagation()}
					class={[
						'min-w-0 truncate text-left hover:underline hover:decoration-fg-faint hover:underline-offset-2',
						broken ? 'text-fg-subtle line-through decoration-red-fg/60' : 'text-fg'
					]}
					title={entry.name}
				>
					{entry.name}
				</button>
				{#if entry.type === 'symlink' && entry.target}
					<span
						class="hidden min-w-0 shrink truncate font-mono text-[11px] text-fg-faint md:inline"
						title="Link to {entry.target}"
					>
						→ {entry.target}
					</span>
				{/if}
			</div>
			{#if folder != null}
				<div class="truncate font-mono text-[10.5px] text-fg-faint">{folder}</div>
			{:else}
				<div class="truncate num text-[10.5px] text-fg-faint sm:hidden">
					{dir ? 'Folder' : bytes(entry.size)} · {ago(modified, now)}
				</div>
			{/if}
		</div>
	</div>
	<div role="gridcell" class="hidden pr-3 text-right num text-[12px] text-fg-muted sm:block">
		{#if dir}<span class="text-fg-faint">—</span>{:else}{bytes(entry.size)}{/if}
	</div>
	<div
		role="gridcell"
		class="hidden truncate pr-3 num text-[12px] text-fg-subtle sm:block"
		title={dateTime(modified)}
	>
		{ago(modified, now)}
	</div>
	<div role="gridcell" class="hidden font-mono text-[11.5px] text-fg-subtle lg:block">
		{entry.mode}
	</div>
	<div
		role="gridcell"
		class="hidden truncate pr-3 text-[12px] text-fg-subtle lg:block"
		title="{entry.owner}:{entry.group}"
	>
		{entry.owner}
	</div>
	<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
	<div
		role="gridcell"
		tabindex="-1"
		class="flex justify-center"
		onclick={(e) => e.stopPropagation()}
		ondblclick={(e) => e.stopPropagation()}
	>
		<Menu>
			{#snippet trigger({ props })}
				<IconButton
					label="Actions for {entry.name}"
					size="sm"
					{...props}
					class={[
						'transition-opacity',
						props['aria-expanded'] !== 'true' &&
							'sm:opacity-0 sm:group-hover:opacity-100 sm:focus:opacity-100'
					]}
				>
					<Ellipsis class="size-4" />
				</IconButton>
			{/snippet}
			{@render menu(entry)}
		</Menu>
	</div>
</div>
