<script lang="ts">
	import './layout.css';
	import { Tooltip } from 'bits-ui';
	import favicon from '#lib/assets/favicon.svg';
	import { canView } from '#lib/access.js';
	import NoAccess from '#lib/shell/NoAccess.svelte';
	import Shell from '#lib/shell/Shell.svelte';
	import type { LayoutProps } from './$types';

	let { data, children }: LayoutProps = $props();
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

<Tooltip.Provider delayDuration={300}>
	{#if canView(data.me)}
		<Shell>{@render children()}</Shell>
	{:else}
		<NoAccess />
	{/if}
</Tooltip.Provider>
