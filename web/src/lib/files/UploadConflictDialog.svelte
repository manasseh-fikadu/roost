<!-- Asked once per upload when some of the names are in the folder already. -->
<script lang="ts">
	import type { ConflictMode } from '#lib/api/types.js';
	import { plural } from '#lib/format.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import FileIcon from './FileIcon.svelte';
	import { basename } from './paths.js';

	interface Props {
		open: boolean;
		/** The names that are there already, and how many files are being uploaded. */
		existing: string[];
		total: number;
		onchoose: (mode: ConflictMode | 'skip') => void;
	}

	let { open = $bindable(), existing, total, onchoose }: Props = $props();
</script>

<Dialog
	bind:open
	title={existing.length === 1
		? `“${basename(existing[0])}” already exists`
		: `${existing.length} files already exist`}
	description={existing.length === total
		? 'Everything you’re uploading has a file with the same name here.'
		: `${plural(total - existing.length, 'other file')} will upload normally.`}
	width="sm:max-w-md"
>
	<ul class="max-h-40 overflow-y-auto rounded-lg border border-line bg-canvas py-1">
		{#each existing.slice(0, 30) as n (n)}
			<li class="flex h-7 items-center gap-2 px-2.5 text-[12.5px] text-fg-muted">
				<FileIcon entry={{ name: basename(n), type: 'file' }} />
				<span class="truncate">{n}</span>
			</li>
		{/each}
		{#if existing.length > 30}
			<li class="px-2.5 py-1 text-[12px] text-fg-faint">and {existing.length - 30} more</li>
		{/if}
	</ul>
	<p class="mt-3 text-[12px] text-fg-subtle">
		“Keep both” saves the new file as “name (1).ext”. “Replace” keeps the old file’s permissions.
	</p>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (open = false)}>Cancel upload</Button>
		<Button onclick={() => onchoose('skip')}>Skip</Button>
		<Button onclick={() => onchoose('rename')}>Keep both</Button>
		<Button variant="primary" onclick={() => onchoose('overwrite')}>Replace</Button>
	{/snippet}
</Dialog>
