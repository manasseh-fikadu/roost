<script lang="ts">
	import { getHealth } from '#lib/api/client.js';

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
	</section>
</main>
