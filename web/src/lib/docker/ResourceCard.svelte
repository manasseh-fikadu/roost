<!--
	A card for a list that a poll loads: volumes, images or networks. It shows
	what the poll has: a placeholder while the first load is on its way, why
	the list failed to load, or that there is nothing, and otherwise the list,
	which is the `children` snippet.
-->
<script lang="ts" generics="T">
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import type { Snippet } from 'svelte';
	import type { Icon } from '#lib/icon.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import ErrorState from '#lib/ui/ErrorState.svelte';
	import type { Poll } from './poll.svelte.js';

	interface Props {
		title: string;
		description: string;
		icon: Icon;
		/** What the card says if the list is empty. */
		empty: string;
		poll: Poll<T[]>;
		/** The list, given what the poll loaded. */
		children: Snippet<[T[]]>;
	}

	let { title, description, icon, empty, poll, children }: Props = $props();

	let refreshing = $state(false);

	async function refresh() {
		refreshing = true;
		try {
			await poll.reload();
		} finally {
			refreshing = false;
		}
	}
</script>

<Card {title} {description}>
	{#snippet actions()}
		{#if poll.error && poll.data}
			<span class="text-[12px] text-amber-fg" title={poll.error}>refresh failed</span>
		{/if}
		<IconButton label="Refresh {title.toLowerCase()}" size="sm" onclick={refresh}>
			<RefreshCw class={['size-3.5', refreshing && 'animate-spin']} />
		</IconButton>
	{/snippet}
	{#if poll.data}
		{#if poll.data.length === 0}
			<EmptyState title={empty} {icon} />
		{:else}
			{@render children(poll.data)}
		{/if}
	{:else if poll.error}
		<ErrorState
			title="Couldn't load {title.toLowerCase()}"
			{icon}
			message={poll.error}
			loading={refreshing}
			onretry={refresh}
		/>
	{:else}
		<div class="space-y-2 p-4">
			<Skeleton class="h-10" />
			<Skeleton class="h-10" />
			<Skeleton class="h-10" />
		</div>
	{/if}
</Card>
