<!--
	A text file in the editor. A file over 5 MB opens a part of it, the start
	or the end, or downloads.
-->
<script lang="ts">
	import Download from '@lucide/svelte/icons/download';
	import type { FileEntry } from '#lib/api/types.js';
	import { bytes } from '#lib/format.js';
	import Button from '#lib/ui/Button.svelte';
	import { errorOf, fetchRaw, rawUrl, startDownload } from './api.js';
	import FileIcon from './FileIcon.svelte';
	import { TEXT_LIMIT, TEXT_PARTIAL } from './kinds.js';
	import LoadError from './LoadError.svelte';
	import PreviewMessage from './PreviewMessage.svelte';
	import Spinner from './Spinner.svelte';

	interface Props {
		device: string;
		entry: FileEntry;
		ondirtychange: (dirty: boolean) => void;
		onsaved: (entry: FileEntry) => void;
	}

	let { device, entry, ondirtychange, onsaved }: Props = $props();

	const path = $derived(entry.path);
	const name = $derived(entry.name);
	const big = $derived(entry.size > TEXT_LIMIT);
	/** Logs are read from the end. */
	const log = $derived(/\.(log|out|err)(\.\d+)?$/i.test(name) || path.startsWith('/var/log/'));

	let part = $state<'head' | 'tail' | null>(null);
	let reloads = $state(0);
	let loaded = $state<{ text: string; mtime: number } | null>(null);
	let failure = $state<{ status: number; message: string } | null>(null);

	$effect(() => {
		// Not keyed on the modified time: a save changes it, and reloading would throw the editor away.
		void [device, path, big, part, reloads];
		if (big && !part) {
			return;
		}
		const ctl = new AbortController();
		loaded = null;
		failure = null;
		fetchRaw(device, path, {
			limit: big ? TEXT_PARTIAL : undefined,
			tail: part === 'tail',
			signal: ctl.signal
		})
			.then(({ data, mtime }) => {
				let text = new TextDecoder().decode(data);
				// A tail starts partway into a line, and maybe into a character: begin at the first full line.
				if (part === 'tail') {
					text = text.slice(text.indexOf('\n') + 1);
				}
				loaded = { text, mtime };
			})
			.catch((err) => {
				if (!ctl.signal.aborted) {
					failure = errorOf(err);
				}
			});
		return () => ctl.abort();
	});
</script>

{#if big && !part}
	<PreviewMessage icon={Download} title="{bytes(entry.size)} is too big to open in the editor">
		Read part of it, or download the whole file.
		<div class="mt-4 flex flex-wrap justify-center gap-2">
			{#snippet first()}
				<Button size="sm" variant={log ? 'secondary' : 'primary'} onclick={() => (part = 'head')}>
					First {bytes(TEXT_PARTIAL)}
				</Button>
			{/snippet}
			{#snippet last()}
				<Button size="sm" variant={log ? 'primary' : 'secondary'} onclick={() => (part = 'tail')}>
					Last {bytes(TEXT_PARTIAL)}
				</Button>
			{/snippet}
			{#if log}
				{@render last()}{@render first()}
			{:else}
				{@render first()}{@render last()}
			{/if}
			<Button
				size="sm"
				variant="ghost"
				onclick={() => startDownload(rawUrl(device, path, { download: true }))}
			>
				{#snippet icon()}<Download class="size-3.5" />{/snippet}
				Download
			</Button>
		</div>
	</PreviewMessage>
{:else if failure}
	<LoadError {...failure} />
{:else if !loaded}
	<Spinner />
{:else}
	{#key `${path}:${reloads}`}
		{#await import('./CodeViewer.svelte')}
			<Spinner />
		{:then { default: CodeViewer }}
			<CodeViewer
				{device}
				{entry}
				text={loaded.text}
				mtime={loaded.mtime}
				partial={big ? part : null}
				{ondirtychange}
				{onsaved}
				onreload={() => {
					ondirtychange(false);
					reloads++;
				}}
			/>
		{:catch}
			<LoadError status={0} message="The editor didn’t load." />
		{/await}
	{/key}
{/if}
