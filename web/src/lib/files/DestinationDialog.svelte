<!-- Picks the folder to move or copy files into, and warns about name clashes before anything runs. -->
<script lang="ts">
	import ArrowUp from '@lucide/svelte/icons/arrow-up';
	import ChevronRight from '@lucide/svelte/icons/chevron-right';
	import CornerDownLeft from '@lucide/svelte/icons/corner-down-left';
	import House from '@lucide/svelte/icons/house';
	import type { ConflictMode, FileEntry, FileListing } from '#lib/api/types.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import { fieldClass } from '#lib/ui/button.js';
	import { errorOf, filesApi } from './api.js';
	import ErrorLine from './ErrorLine.svelte';
	import FileIcon from './FileIcon.svelte';
	import { isDirLike } from './kinds.js';
	import { joinPath, tildify } from './paths.js';

	interface Props {
		open: boolean;
		mode: 'move' | 'copy';
		device: string;
		items: FileEntry[];
		/** The folder to show first. */
		start: string;
		home: string | null;
		/** Does the work in the folder. If it throws, the dialog stays open and says why. */
		onsubmit: (dest: string, conflict: ConflictMode | 'skip') => Promise<void>;
	}

	let { open = $bindable(), mode, device, items, start, home, onsubmit }: Props = $props();

	let dir = $state('');
	let typed = $state('');
	let listing = $state<FileListing | null>(null);
	let error = $state<string | null>(null);
	let submitError = $state<string | null>(null);
	let conflict = $state<ConflictMode | 'skip'>('rename');
	let busy = $state(false);

	$effect(() => {
		if (open) {
			dir = start;
			typed = start;
			submitError = null;
		}
	});

	$effect(() => {
		if (!open) {
			return;
		}
		const wanted = dir;
		let current = true;
		listing = null;
		error = null;
		filesApi
			.list(device, wanted)
			.then((l) => {
				if (current) {
					listing = l;
					typed = l.path;
				}
			})
			.catch((err) => {
				if (current) {
					error = errorOf(err).message;
				}
			});
		return () => {
			current = false;
		};
	});

	const folders = $derived(
		(listing?.entries ?? []).filter(isDirLike).sort((a, b) => a.name.localeCompare(b.name))
	);
	const names = $derived(new Set((listing?.entries ?? []).map((e) => e.name)));
	const clashes = $derived(
		listing
			? items.filter((i) => names.has(i.name) && joinPath(listing!.path, i.name) !== i.path)
			: []
	);
	const sameFolder = $derived(
		listing ? items.every((i) => joinPath(listing!.path, i.name) === i.path) : false
	);
	const intoSelf = $derived(
		listing
			? items.some((i) => listing!.path === i.path || listing!.path.startsWith(`${i.path}/`))
			: false
	);
	const verb = $derived(mode === 'move' ? 'Move' : 'Copy');
	const what = $derived(items.length === 1 ? `“${items[0].name}”` : `${items.length} items`);

	async function submit() {
		if (!listing) {
			return;
		}
		busy = true;
		submitError = null;
		try {
			await onsubmit(listing.path, clashes.length ? conflict : 'fail');
			open = false;
		} catch (err) {
			submitError = errorOf(err).message;
		} finally {
			busy = false;
		}
	}
</script>

<Dialog
	bind:open
	title="{verb} {what}"
	description="Pick a destination folder, or type a path."
	width="sm:max-w-lg"
>
	<form
		class="flex gap-2"
		onsubmit={(e) => {
			e.preventDefault();
			const v = typed.trim();
			if (v) {
				dir = v.startsWith('~') && home ? home + v.slice(1) : v;
			}
		}}
	>
		<input
			bind:value={typed}
			spellcheck="false"
			aria-label="Destination path"
			class={[fieldClass, 'h-8 font-mono text-[12.5px]']}
		/>
		<Button type="submit" aria-label="Go"><CornerDownLeft class="size-3.5" /></Button>
	</form>
	<div class="mt-2 flex items-center gap-1 text-[12px]">
		<Button
			size="xs"
			variant="ghost"
			disabled={!listing?.parent}
			onclick={() => listing?.parent && (dir = listing.parent)}
		>
			{#snippet icon()}<ArrowUp class="size-3" />{/snippet}
			Up
		</Button>
		{#if home}
			<Button size="xs" variant="ghost" onclick={() => (dir = home)}>
				{#snippet icon()}<House class="size-3" />{/snippet}
				Home
			</Button>
		{/if}
		<span class="ml-auto truncate font-mono text-[11px] text-fg-faint">
			{listing ? tildify(listing.path, home) : ''}
		</span>
	</div>
	<div class="mt-1.5 h-64 overflow-y-auto rounded-lg border border-line bg-canvas">
		{#if error}
			<div class="p-3 text-[12.5px] text-red-fg">{error}</div>
		{:else if !listing}
			<div class="space-y-1.5 p-2">
				{#each { length: 6 }, i (i)}<Skeleton class="h-7" />{/each}
			</div>
		{:else if folders.length === 0}
			<div class="p-6 text-center text-[12.5px] text-fg-subtle">
				No folders inside. {verb} into this one, or go up.
			</div>
		{:else}
			{#each folders as f (f.path)}
				<button
					type="button"
					disabled={items.some((i) => i.path === f.path)}
					onclick={() => (dir = f.path)}
					class="flex h-8 w-full items-center gap-2 px-2.5 text-left text-[13px] text-fg-muted hover:bg-hover hover:text-fg disabled:opacity-40"
				>
					<FileIcon entry={f} />
					<span class="flex-1 truncate">{f.name}</span>
					<ChevronRight class="size-3.5 text-fg-faint" />
				</button>
			{/each}
		{/if}
	</div>
	{#if intoSelf}
		<ErrorLine>A folder can’t be {mode === 'move' ? 'moved' : 'copied'} into itself.</ErrorLine>
	{:else if mode === 'move' && sameFolder}
		<p class="mt-3 text-[12.5px] text-fg-subtle">
			That’s where {items.length === 1 ? 'it already is' : 'they already are'}.
		</p>
	{/if}
	{#if clashes.length > 0 && !intoSelf}
		<div class="mt-3 rounded-lg border border-amber/25 bg-amber/[0.05] p-3">
			<div class="text-[12.5px] text-fg">
				{clashes.length === 1
					? `“${clashes[0].name}” already exists here.`
					: `${clashes.length} of these names already exist here.`}
			</div>
			<div class="mt-2">
				<Segmented
					bind:value={conflict}
					label="When a name exists"
					options={[
						{ value: 'rename', label: 'Keep both' },
						{ value: 'overwrite', label: 'Replace' },
						{ value: 'skip', label: 'Skip' }
					]}
				/>
			</div>
			{#if conflict === 'overwrite'}
				<p class="mt-2 text-[11.5px] text-fg-subtle">
					Files are replaced; folders with the same name are skipped.
				</p>
			{/if}
		</div>
	{/if}
	{#if submitError}<ErrorLine>{submitError}</ErrorLine>{/if}
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
		<Button
			variant="primary"
			loading={busy}
			disabled={!listing ||
				intoSelf ||
				(mode === 'move' && sameFolder) ||
				(clashes.length === items.length && conflict === 'skip')}
			onclick={submit}
		>
			{verb} here
		</Button>
	{/snippet}
</Dialog>
