<!-- What a list or page shows when it has nothing in it: a title, a line about why, and what to do. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { Icon } from '#lib/icon.js';

	interface Props {
		title: string;
		icon?: Icon;
		/** The explanation, and any action. */
		children?: Snippet;
	}

	let { title, icon: IconComponent, children }: Props = $props();
</script>

<div class="flex flex-col items-center justify-center px-6 py-12 text-center">
	{#if IconComponent}
		<div
			class="mb-3 flex size-10 items-center justify-center rounded-full border border-line bg-raised text-fg-subtle"
		>
			<IconComponent class="size-4" strokeWidth={1.75} />
		</div>
	{/if}
	<p class="text-[14px] font-medium text-fg">{title}</p>
	{#if children}
		<div class="mt-1 max-w-sm text-[13px] text-fg-subtle">{@render children()}</div>
	{/if}
</div>
