<!-- A square button that holds only an icon, with its label as a tooltip. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import Tooltip from './Tooltip.svelte';
	import { iconButtonClass, type IconButtonSize } from './button.js';
	import { mergeProps } from './props.js';

	interface Props extends HTMLButtonAttributes {
		/** What the button does, for screen readers and the tooltip. */
		label: string;
		size?: IconButtonSize;
		children: Snippet;
	}

	let {
		label,
		size = 'md',
		children,
		class: className,
		type = 'button',
		...rest
	}: Props = $props();
</script>

<Tooltip content={label}>
	{#snippet trigger({ props })}
		<button
			{...mergeProps(props, rest, {
				type,
				'aria-label': label,
				class: [iconButtonClass(size), className]
			})}
		>
			{@render children()}
		</button>
	{/snippet}
</Tooltip>
