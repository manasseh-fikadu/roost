<!-- A Word document or a spreadsheet, which the browser reads whole and a viewer draws. -->
<script lang="ts">
	import type { FileEntry } from '#lib/api/types.js';
	import { bytes } from '#lib/format.js';
	import { errorOf, fetchRaw } from './api.js';
	import LoadError from './LoadError.svelte';
	import NoPreview from './NoPreview.svelte';
	import Spinner from './Spinner.svelte';

	interface Props {
		device: string;
		entry: FileEntry;
		kind: 'docx' | 'sheet';
	}

	let { device, entry, kind }: Props = $props();

	/** The biggest file the browser reads whole to draw. */
	const LIMIT = 60 * 1024 * 1024;

	const path = $derived(entry.path);
	const tooBig = $derived(entry.size > LIMIT);

	let data = $state<ArrayBuffer | null>(null);
	let failure = $state<{ status: number; message: string } | null>(null);

	$effect(() => {
		void [device, path, entry.modifiedAt];
		if (tooBig) {
			return;
		}
		const ctl = new AbortController();
		data = null;
		failure = null;
		fetchRaw(device, path, { signal: ctl.signal })
			.then((r) => (data = r.data))
			.catch((err) => {
				if (!ctl.signal.aborted) {
					failure = errorOf(err);
				}
			});
		return () => ctl.abort();
	});
</script>

{#if tooBig}
	<NoPreview {device} {entry} reason="{bytes(entry.size)} is too big to preview in the browser." />
{:else if failure}
	<LoadError {...failure} />
{:else if !data}
	<Spinner label="Loading {bytes(entry.size)}…" />
{:else if kind === 'docx'}
	{#await import('./DocxViewer.svelte')}
		<Spinner />
	{:then { default: DocxViewer }}
		<DocxViewer {data} />
	{:catch}
		<LoadError status={0} message="The viewer didn’t load." />
	{/await}
{:else}
	{#await import('./SheetViewer.svelte')}
		<Spinner />
	{:then { default: SheetViewer }}
		<SheetViewer {data} name={entry.name} />
	{:catch}
		<LoadError status={0} message="The viewer didn’t load." />
	{/await}
{/if}
