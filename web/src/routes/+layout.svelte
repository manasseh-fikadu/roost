<script lang="ts">
	import './layout.css';
	import favicon from '#lib/assets/favicon.svg';
	import { addViewerCommand, canView, whyCantView } from '#lib/access.js';
	import type { LayoutProps } from './$types';

	let { data, children }: LayoutProps = $props();

	const viewable = $derived(canView(data.me));
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	{#if !viewable}
		<title>Roost</title>
	{/if}
</svelte:head>

{#if viewable}
	{@render children()}
{:else}
	<main class="grid min-h-dvh place-items-center bg-stone-50 p-6 text-stone-900">
		<section class="w-full max-w-sm rounded-xl border border-stone-200 bg-white p-6 shadow-sm">
			<h1 class="text-lg font-semibold">Roost</h1>
			{#if data.me}
				{@const command = addViewerCommand(data.me)}
				<p class="mt-2 text-sm text-stone-700">{whyCantView(data.me)}</p>
				{#if command}
					<p class="mt-3 text-sm text-stone-500">They can run this on the machine Roost runs on:</p>
					<code
						class="mt-1 block rounded-md bg-stone-100 px-2 py-1.5 font-mono text-xs break-all text-stone-800"
						>{command}</code
					>
				{/if}
			{:else}
				<p class="mt-2 flex items-center gap-2 text-sm text-red-700">
					<span class="size-2 shrink-0 rounded-full bg-red-500" aria-hidden="true"></span>
					Can't reach the hub: {data.hubError}
				</p>
			{/if}
		</section>
	</main>
{/if}
