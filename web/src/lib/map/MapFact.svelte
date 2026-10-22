<!-- A labelled value on a <MapCard>, such as RTT 17 ms. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		label: string;
		/** Takes the card's whole width. */
		wide?: boolean;
		children: Snippet;
	}

	let { label, wide = false, children }: Props = $props();
</script>

<div class={['flex min-w-0 items-baseline gap-1.5', wide && 'col-span-2']}>
	<dt class="shrink-0 font-mono text-[10px] tracking-wide text-fg-faint uppercase">{label}</dt>
	<dd class="truncate num text-[11.5px] text-fg">{@render children()}</dd>
</div>
