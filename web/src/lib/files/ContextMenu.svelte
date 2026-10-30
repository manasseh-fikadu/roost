<!--
	A menu at the pointer, for a right-click on a row. It's a <Menu> without a
	button: it lines up with a point instead, so its items are <MenuItem>s.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { DropdownMenu } from 'bits-ui';

	interface Props {
		open: boolean;
		/** Where the click was, in window coordinates. */
		x: number;
		y: number;
		children: Snippet;
	}

	let { open = $bindable(), x, y, children }: Props = $props();

	/** A zero-size box at the pointer, for the menu to line up with. */
	const anchor = $derived({
		getBoundingClientRect: () => new DOMRect(x, y, 0, 0)
	});
</script>

<DropdownMenu.Root bind:open>
	<DropdownMenu.Portal>
		<DropdownMenu.Content
			customAnchor={anchor}
			align="start"
			side="bottom"
			sideOffset={2}
			class="z-50 min-w-48 pop rounded-lg border border-line-strong bg-surface p-1 shadow-popover outline-none"
			oncontextmenu={(e: MouseEvent) => e.preventDefault()}
		>
			{@render children()}
		</DropdownMenu.Content>
	</DropdownMenu.Portal>
</DropdownMenu.Root>
