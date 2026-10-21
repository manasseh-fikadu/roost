<script lang="ts">
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { roleLabel } from '#lib/access.js';
	import { getHealth } from '#lib/api/client.js';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const health = getHealth();
</script>

<svelte:head>
	<title>Roost</title>
</svelte:head>

<main class="grid min-h-dvh place-items-center bg-stone-50 p-6 text-stone-900">
	<section class="w-full max-w-sm rounded-xl border border-stone-200 bg-white p-6 shadow-sm">
		<h1 class="text-lg font-semibold">Roost</h1>
		{#await health}
			<p class="mt-2 text-sm text-stone-500">Checking the hub…</p>
		{:then { status, version }}
			<p class="mt-2 flex items-center gap-2 text-sm">
				<span class="size-2 rounded-full bg-emerald-500" aria-hidden="true"></span>
				Hub is {status}, version {version}
			</p>
		{:catch error}
			<p class="mt-2 flex items-center gap-2 text-sm text-red-700">
				<span class="size-2 rounded-full bg-red-500" aria-hidden="true"></span>
				Can't reach the hub: {error.message}
			</p>
		{/await}

		{#if data.me?.role}
			{@const { name, login } = data.me}
			<div
				class="mt-4 flex items-start justify-between gap-3 border-t border-stone-100 pt-4 text-sm"
			>
				<div class="min-w-0">
					<p class="text-stone-500">Signed in as</p>
					<p class="truncate font-medium">{name || login}</p>
					{#if name && login && name !== login}
						<p class="truncate text-stone-500">{login}</p>
					{/if}
				</div>
				<span
					class="shrink-0 rounded-full bg-stone-100 px-2 py-0.5 text-xs font-medium text-stone-700"
				>
					{roleLabel(data.me.role)}
				</span>
			</div>
		{/if}

		<div class="mt-4 border-t border-stone-100 pt-4 text-sm">
			<AdminOnly>
				<!-- An API URL, not an app route, so the browser loads it, not SvelteKit's router. -->
				<a
					href="/api/debug/tailnet"
					data-sveltekit-reload
					class="font-medium text-stone-700 underline decoration-stone-300 underline-offset-4 hover:text-stone-900 hover:decoration-stone-500"
				>
					Tailnet debug info
				</a>
				{#snippet fallback()}
					<p class="text-stone-500">Admins see more here.</p>
				{/snippet}
			</AdminOnly>
		</div>
	</section>
</main>
