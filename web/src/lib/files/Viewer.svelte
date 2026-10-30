<!-- Picks how to show a file by what it is. -->
<script lang="ts">
	import AlertTriangle from '@lucide/svelte/icons/triangle-alert';
	import Music from '@lucide/svelte/icons/music';
	import type { FileEntry } from '#lib/api/types.js';
	import { rawUrl } from './api.js';
	import BinaryView from './BinaryView.svelte';
	import ImageView from './ImageView.svelte';
	import { previewKind } from './kinds.js';
	import NoPreview from './NoPreview.svelte';
	import PreviewMessage from './PreviewMessage.svelte';
	import SniffView from './SniffView.svelte';
	import TextView from './TextView.svelte';

	interface Props {
		device: string;
		entry: FileEntry;
		ondirtychange: (dirty: boolean) => void;
		onsaved: (entry: FileEntry) => void;
	}

	let { device, entry, ondirtychange, onsaved }: Props = $props();

	const src = $derived(rawUrl(device, entry.path));
	const kind = $derived(previewKind(entry.name));
</script>

{#if entry.type === 'other'}
	<NoPreview {device} {entry} reason="This is a device, socket or pipe, not a regular file." />
{:else if entry.type === 'symlink' && !entry.targetType}
	<PreviewMessage icon={AlertTriangle} title="Broken link" tone="error">
		It points to {entry.target ?? 'something'} that doesn’t exist.
	</PreviewMessage>
{:else if kind === 'image'}
	<ImageView {src} name={entry.name} />
{:else if kind === 'video'}
	<div class="flex size-full items-center justify-center bg-black p-2">
		{#key src}
			<!-- svelte-ignore a11y_media_has_caption -->
			<video {src} controls playsinline preload="metadata" class="max-h-full max-w-full"></video>
		{/key}
	</div>
{:else if kind === 'audio'}
	<PreviewMessage icon={Music} title={entry.name}>
		{#key src}
			<!-- svelte-ignore a11y_media_has_caption -->
			<audio {src} controls preload="metadata" class="mt-4 w-[min(420px,80vw)]"></audio>
		{/key}
	</PreviewMessage>
{:else if kind === 'pdf'}
	{#key src}
		<iframe {src} title={entry.name} class="size-full border-0 bg-[#525659]"></iframe>
	{/key}
{:else if kind === 'text' || kind === 'markdown'}
	<TextView {device} {entry} {ondirtychange} {onsaved} />
{:else if kind === 'docx' || kind === 'sheet'}
	<BinaryView {device} {entry} {kind} />
{:else}
	<SniffView {device} {entry} {ondirtychange} {onsaved} />
{/if}
