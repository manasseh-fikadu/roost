<!--
	One fact in a card's list, such as "Tailnet IP 100.64.0.2", with a button
	to copy it if `copy` is given. Put the rows in a <dl>.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import CopyButton from './CopyButton.svelte';

	interface Props {
		label: string;
		/** The text the copy button copies. */
		copy?: string;
		children: Snippet;
	}

	let { label, copy, children }: Props = $props();
</script>

<div
	class="flex min-h-9 items-center justify-between gap-4 border-b border-line px-4 py-1.5 last:border-0"
>
	<dt class="shrink-0 text-[13px] text-fg-subtle">{label}</dt>
	<dd class="flex min-w-0 items-center gap-1 text-right">
		<span class="truncate num text-[12.5px] text-fg">{@render children()}</span>
		{#if copy}
			<span class="-mr-2"><CopyButton value={copy} label="Copy {label}" /></span>
		{/if}
	</dd>
</div>
