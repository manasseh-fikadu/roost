<!-- The title at the top of a page, with a line about it and its actions. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		description?: string;
		actions?: Snippet;
	}

	let { title, description, actions }: Props = $props();
</script>

<div class="mb-6 flex flex-wrap items-end justify-between gap-4">
	<div class="min-w-0">
		<h1 class="text-[24px] leading-8 font-semibold tracking-[-0.02em] text-fg">{title}</h1>
		{#if description}
			<p class="mt-1 text-[14px] text-fg-muted">{description}</p>
		{/if}
	</div>
	{#if actions}
		<div class="flex flex-wrap items-center gap-2">{@render actions()}</div>
	{/if}
</div>
