<!-- Why a file couldn't be read, in a preview. -->
<script lang="ts">
	import Lock from '@lucide/svelte/icons/lock';
	import AlertTriangle from '@lucide/svelte/icons/triangle-alert';
	import PreviewMessage from './PreviewMessage.svelte';

	let { status, message }: { status: number; message: string } = $props();
</script>

<PreviewMessage
	icon={status === 403 ? Lock : AlertTriangle}
	title={status === 403 ? 'No permission to read this file' : 'Couldn’t open this file'}
	tone="error"
>
	<span class="font-mono text-[12px]">{message}</span>
</PreviewMessage>
