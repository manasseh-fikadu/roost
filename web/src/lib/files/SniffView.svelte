<!-- A file with no known type: look at its first bytes, and show text if that's what it is. -->
<script lang="ts">
	import type { FileEntry } from '#lib/api/types.js';
	import { errorOf, fetchRaw } from './api.js';
	import { looksLikeText } from './kinds.js';
	import LoadError from './LoadError.svelte';
	import NoPreview from './NoPreview.svelte';
	import Spinner from './Spinner.svelte';
	import TextView from './TextView.svelte';

	interface Props {
		device: string;
		entry: FileEntry;
		ondirtychange: (dirty: boolean) => void;
		onsaved: (entry: FileEntry) => void;
	}

	let { device, entry, ondirtychange, onsaved }: Props = $props();

	const path = $derived(entry.path);
	const empty = $derived(entry.size === 0);

	let verdict = $state<'text' | 'binary' | null>(null);
	let failure = $state<{ status: number; message: string } | null>(null);

	$effect(() => {
		void [device, path];
		failure = null;
		if (empty) {
			verdict = 'text';
			return;
		}
		verdict = null;
		const ctl = new AbortController();
		fetchRaw(device, path, { limit: 8192, signal: ctl.signal })
			.then(({ data }) => (verdict = looksLikeText(new Uint8Array(data)) ? 'text' : 'binary'))
			.catch((err) => {
				if (!ctl.signal.aborted) {
					failure = errorOf(err);
				}
			});
		return () => ctl.abort();
	});
</script>

{#if failure}
	<LoadError {...failure} />
{:else if verdict === 'text'}
	<TextView {device} {entry} {ondirtychange} {onsaved} />
{:else if verdict === 'binary'}
	<NoPreview {device} {entry} reason="This looks like a binary file." />
{:else}
	<Spinner />
{/if}
