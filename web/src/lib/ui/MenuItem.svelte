<!-- An item in a <Menu>: an action, or a link if it has an href. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { DropdownMenu } from 'bits-ui';
	import type { Icon } from '#lib/icon.js';

	interface Props {
		onSelect?: () => void;
		/** Makes the item a link. */
		href?: string;
		/** Loads href as a page of its own, not through the app's router. */
		reload?: boolean;
		/** For actions that destroy or remove something. */
		danger?: boolean;
		disabled?: boolean;
		icon?: Icon;
		children: Snippet;
	}

	let {
		onSelect,
		href,
		reload = false,
		danger = false,
		disabled = false,
		icon: IconComponent,
		children
	}: Props = $props();

	const itemClass = $derived([
		'flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-[13px] outline-none transition-colors select-none focus-visible:shadow-none data-disabled:opacity-50',
		danger
			? 'text-red-fg data-highlighted:bg-red/10'
			: 'text-fg-muted data-highlighted:bg-hover data-highlighted:text-fg'
	]);
</script>

{#snippet content()}
	{#if IconComponent}
		<IconComponent class="size-4 shrink-0" />
	{/if}
	{@render children()}
{/snippet}

{#if href}
	<DropdownMenu.Item {onSelect} {disabled}>
		{#snippet child({ props })}
			<a {...props} {href} data-sveltekit-reload={reload || undefined} class={itemClass}>
				{@render content()}
			</a>
		{/snippet}
	</DropdownMenu.Item>
{:else}
	<DropdownMenu.Item {onSelect} {disabled} class={itemClass}>
		{@render content()}
	</DropdownMenu.Item>
{/if}
