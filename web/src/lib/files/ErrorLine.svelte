<!-- Why an action in a dialog failed, under the form. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import AlertTriangle from '@lucide/svelte/icons/triangle-alert';

	let { children }: { children: Snippet } = $props();
</script>

<div
	role="alert"
	class="mt-3 flex items-start gap-2 rounded-md border border-red/30 bg-red/[0.06] px-3 py-2 text-[12.5px] text-red-fg"
>
	<AlertTriangle class="mt-0.5 size-3.5 shrink-0" />
	<span class="min-w-0 break-words">{@render children()}</span>
</div>
