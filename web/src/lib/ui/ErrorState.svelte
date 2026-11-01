<!--
	What a list or page shows when it couldn't load: what failed, the reason
	the hub or the browser gave, and a button to try again. `actions` adds
	other buttons beside it, such as a link back.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { Icon } from '#lib/icon.js';
	import Button from './Button.svelte';
	import EmptyState from './EmptyState.svelte';

	interface Props {
		/** What went wrong, in a few words, such as "Couldn't load the channels". */
		title: string;
		/** The reason, as it came back. */
		message?: string;
		icon?: Icon;
		/** Tries again. Without it there's no button. */
		onretry?: () => void;
		/** Whether the try that's under way. */
		loading?: boolean;
		actions?: Snippet;
	}

	let { title, message, icon, onretry, loading = false, actions }: Props = $props();
</script>

<div role="alert">
	<EmptyState {title} {icon}>
		{#if message}
			<span class="block font-mono text-[12px] break-words text-fg-subtle">{message}</span>
		{/if}
		{#if onretry || actions}
			<div class="mt-3 flex flex-wrap items-center justify-center gap-2">
				{#if onretry}
					<Button {loading} onclick={onretry}>Try again</Button>
				{/if}
				{@render actions?.()}
			</div>
		{/if}
	</EmptyState>
</div>
