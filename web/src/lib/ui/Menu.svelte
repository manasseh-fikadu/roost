<!--
	A dropdown menu. Render the button that opens it in the `trigger` snippet
	and spread `props` on it, and put <MenuItem>s inside. Arrow keys move
	between items, typing jumps to one, and Escape closes it.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { DropdownMenu } from 'bits-ui';

	interface Props {
		open?: boolean;
		trigger: Snippet<[{ props: Record<string, unknown> }]>;
		children: Snippet;
		/** Which edge of the trigger the menu lines up with. */
		align?: 'start' | 'center' | 'end';
		class?: string;
	}

	let {
		open = $bindable(false),
		trigger,
		children,
		align = 'end',
		class: className
	}: Props = $props();
</script>

<DropdownMenu.Root bind:open>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			{@render trigger({ props })}
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Portal>
		<DropdownMenu.Content
			{align}
			sideOffset={6}
			class={[
				'z-50 min-w-44 pop rounded-lg border border-line-strong bg-surface p-1 shadow-popover outline-none',
				className
			]}
		>
			{@render children()}
		</DropdownMenu.Content>
	</DropdownMenu.Portal>
</DropdownMenu.Root>
