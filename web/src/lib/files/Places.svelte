<!-- Shortcuts to folders and the bookmarked ones, with the disk's free space. The sidebar and the phone's menu both show it. -->
<script lang="ts">
	import Star from '@lucide/svelte/icons/star';
	import X from '@lucide/svelte/icons/x';
	import type { FileSpace } from '#lib/api/types.js';
	import { bytes } from '#lib/format.js';
	import Meter from '#lib/ui/Meter.svelte';
	import { basename, tildify } from './paths.js';
	import type { Place } from './places.js';

	interface Props {
		places: Place[];
		bookmarks: string[];
		/** The folder that's open. */
		current: string | null;
		home: string | null;
		space: FileSpace | null | undefined;
		ongo: (path: string) => void;
		onremovebookmark: (path: string) => void;
	}

	let { places, bookmarks, current, home, space, ongo, onremovebookmark }: Props = $props();

	const used = $derived(
		space && space.total > 0 ? ((space.total - space.free) / space.total) * 100 : null
	);

	const link =
		'flex h-8 w-full items-center gap-2.5 rounded-md px-2 text-left text-[13px] transition-colors';
</script>

<div class="flex flex-col gap-4">
	<div>
		<div class="px-2 pb-1.5 font-mono text-[10.5px] tracking-wider text-fg-faint uppercase">
			Places
		</div>
		{#each places as p (p.path)}
			{@const active = current === p.path}
			<button
				type="button"
				title={p.path}
				onclick={() => ongo(p.path)}
				class={[
					link,
					active ? 'bg-hover text-fg' : 'text-fg-subtle hover:bg-hover/60 hover:text-fg'
				]}
			>
				<p.icon class={['size-3.5', active ? 'text-fg' : 'text-fg-faint']} />
				<span class="truncate">{p.label}</span>
			</button>
		{/each}
	</div>
	<div>
		<div class="px-2 pb-1.5 font-mono text-[10.5px] tracking-wider text-fg-faint uppercase">
			Bookmarks
		</div>
		{#if bookmarks.length === 0}
			<p class="px-2 text-[12px] leading-5 text-fg-faint">Star a folder to pin it here.</p>
		{:else}
			{#each bookmarks as b (b)}
				{@const active = current === b}
				{@const label = b === '/' ? '/' : basename(b)}
				<div class="group relative">
					<button
						type="button"
						title={tildify(b, home)}
						onclick={() => ongo(b)}
						class={[
							link,
							'pr-7',
							active ? 'bg-hover text-fg' : 'text-fg-subtle hover:bg-hover/60 hover:text-fg'
						]}
					>
						<Star class={['size-3.5', active ? 'text-fg' : 'text-fg-faint']} />
						<span class="truncate">{label}</span>
					</button>
					<button
						type="button"
						onclick={() => onremovebookmark(b)}
						aria-label="Remove bookmark {label}"
						title="Remove bookmark"
						class="absolute top-1/2 right-1 flex size-6 -translate-y-1/2 items-center justify-center rounded text-fg-faint opacity-0 group-hover:opacity-100 hover:bg-line-strong hover:text-fg focus:opacity-100 max-lg:opacity-100"
					>
						<X class="size-3" />
					</button>
				</div>
			{/each}
		{/if}
	</div>
	{#if space && used !== null}
		<div class="px-2">
			<div class="mb-1.5 flex items-baseline justify-between gap-2 text-[11.5px]">
				<span class="text-fg-subtle">Disk</span>
				<span class="num text-fg-muted">{bytes(space.free)} free</span>
			</div>
			<Meter value={used} warn={85} crit={95} />
			<div class="mt-1 num text-[10.5px] text-fg-faint">of {bytes(space.total)}</div>
		</div>
	{/if}
</div>
