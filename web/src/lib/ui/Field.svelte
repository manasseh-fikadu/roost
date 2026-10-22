<!-- A labelled form control, with an optional hint under it. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		label: string;
		hint?: string;
		/** The control. */
		children: Snippet;
	}

	let { label, hint, children }: Props = $props();
</script>

<label class="block">
	<span class="mb-1.5 block text-[13px] font-medium text-fg-muted">{label}</span>
	{@render children()}
	{#if hint}
		<span class="mt-1.5 block text-[12px] text-fg-subtle">{hint}</span>
	{/if}
</label>
