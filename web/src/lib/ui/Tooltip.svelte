<!--
	A short label that shows when the pointer rests on, or focus lands on, its
	trigger. Render the trigger in the `trigger` snippet and spread `props` on
	it. The root layout provides the delay.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Tooltip } from 'bits-ui';

	interface Props {
		content: string;
		side?: 'top' | 'right' | 'bottom' | 'left';
		trigger: Snippet<[{ props: Record<string, unknown> }]>;
	}

	let { content, side = 'top', trigger }: Props = $props();
</script>

<Tooltip.Root>
	<Tooltip.Trigger>
		{#snippet child({ props })}
			{@render trigger({ props })}
		{/snippet}
	</Tooltip.Trigger>
	<Tooltip.Portal>
		<Tooltip.Content
			{side}
			sideOffset={6}
			class="z-[70] rounded-md border border-line-strong bg-raised px-2 py-1 text-[12px] text-fg fade shadow-popover"
		>
			{content}
		</Tooltip.Content>
	</Tooltip.Portal>
</Tooltip.Root>
