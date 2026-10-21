<!--
	A button. For a link that looks like one, put buttonClass() from
	#lib/ui/button.ts on the <a>.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import { buttonClass, type ButtonSize, type ButtonVariant } from './button.js';

	interface Props extends HTMLButtonAttributes {
		variant?: ButtonVariant;
		size?: ButtonSize;
		/** Shows a spinner in place of the icon and turns the button off. */
		loading?: boolean;
		icon?: Snippet;
	}

	let {
		variant = 'secondary',
		size = 'sm',
		loading = false,
		icon,
		children,
		class: className,
		disabled,
		type = 'button',
		...rest
	}: Props = $props();
</script>

<button
	{type}
	disabled={disabled || loading}
	aria-busy={loading || undefined}
	class={[buttonClass(variant, size), className]}
	{...rest}
>
	{#if loading}
		<span
			class="size-3.5 animate-spin rounded-full border-2 border-current border-t-transparent"
			aria-hidden="true"
		></span>
	{:else}
		{@render icon?.()}
	{/if}
	{@render children?.()}
</button>
