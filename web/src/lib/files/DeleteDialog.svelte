<!-- Asks before deleting files and folders, and lists them. -->
<script lang="ts">
	import type { FileEntry } from '#lib/api/types.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import { errorOf } from './api.js';
	import ErrorLine from './ErrorLine.svelte';
	import FileIcon from './FileIcon.svelte';

	interface Props {
		open: boolean;
		items: FileEntry[];
		/** Does the work. If it throws, the dialog stays open and says why. */
		onconfirm: () => Promise<void>;
	}

	let { open = $bindable(), items, onconfirm }: Props = $props();

	let busy = $state(false);
	let error = $state<string | null>(null);

	$effect(() => {
		if (open) {
			error = null;
		}
	});

	const folders = $derived(items.filter((i) => i.type === 'dir').length);

	async function run() {
		busy = true;
		error = null;
		try {
			await onconfirm();
			open = false;
		} catch (err) {
			error = errorOf(err).message;
		} finally {
			busy = false;
		}
	}
</script>

<Dialog
	bind:open
	title={items.length === 1 ? `Delete “${items[0]?.name}”?` : `Delete ${items.length} items?`}
	description={folders
		? 'Folders are deleted with everything inside them. This can’t be undone.'
		: 'This can’t be undone.'}
	width="sm:max-w-md"
>
	<ul class="max-h-48 overflow-y-auto rounded-lg border border-line bg-canvas py-1">
		{#each items.slice(0, 50) as i (i.path)}
			<li class="flex h-7 items-center gap-2 px-2.5 text-[12.5px]">
				<FileIcon entry={i} />
				<span class="truncate text-fg-muted">{i.name}</span>
				{#if i.type === 'symlink'}<span class="text-[11px] text-fg-faint">(link only)</span>{/if}
			</li>
		{/each}
		{#if items.length > 50}
			<li class="px-2.5 py-1 text-[12px] text-fg-faint">and {items.length - 50} more</li>
		{/if}
	</ul>
	{#if error}<ErrorLine>{error}</ErrorLine>{/if}
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
		<Button variant="danger" loading={busy} onclick={run}>Delete</Button>
	{/snippet}
</Dialog>
