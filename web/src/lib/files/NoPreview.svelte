<!-- What a preview says for a file it can't show, with a button to download it. -->
<script lang="ts">
	import Download from '@lucide/svelte/icons/download';
	import FileQuestion from '@lucide/svelte/icons/file-question-mark';
	import type { FileEntry } from '#lib/api/types.js';
	import { bytes } from '#lib/format.js';
	import Button from '#lib/ui/Button.svelte';
	import { rawUrl, startDownload } from './api.js';
	import PreviewMessage from './PreviewMessage.svelte';

	let { device, entry, reason }: { device: string; entry: FileEntry; reason: string } = $props();
</script>

<PreviewMessage icon={FileQuestion} title="No preview for this file">
	{reason}
	<div class="mt-4 flex justify-center">
		<Button
			size="sm"
			variant="primary"
			onclick={() => startDownload(rawUrl(device, entry.path, { download: true }))}
		>
			{#snippet icon()}<Download class="size-3.5" />{/snippet}
			Download {bytes(entry.size)}
		</Button>
	</div>
</PreviewMessage>
