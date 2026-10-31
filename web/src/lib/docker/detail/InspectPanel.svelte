<!--
	A container's whole inspection, as Docker writes it, loaded when the tab
	opens and again on Refresh. A search turns the document into the list of
	values that match, each with its path. Admins only; the page decides.
-->
<script lang="ts">
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import Search from '@lucide/svelte/icons/search';
	import { plural } from '#lib/format.js';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import CopyButton from '#lib/ui/CopyButton.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import { getDockerInspect } from '../api.js';
	import { jsonTokens, searchJson, type JsonKind } from './helpers.js';

	interface Props {
		deviceId: string;
		/** The container's name. */
		container: string;
	}

	let { deviceId, container }: Props = $props();

	/** How many matches a search lists. */
	const maxHits = 200;

	const kindClass: Record<JsonKind, string> = {
		key: 'text-blue-fg',
		string: 'text-green-fg',
		literal: 'text-purple-fg',
		number: 'text-amber-fg',
		plain: ''
	};

	let doc = $state.raw<unknown>(null);
	let loaded = $state(false);
	let error = $state<string | null>(null);
	let loading = $state(true);
	let query = $state('');
	/** Counts the loads, so only the latest one counts. */
	let latest = 0;

	async function load() {
		const mine = ++latest;
		loading = true;
		try {
			const value = await getDockerInspect(deviceId, container);
			if (mine === latest) {
				doc = value;
				loaded = true;
				error = null;
			}
		} catch (err) {
			if (mine === latest) {
				error = err instanceof Error ? err.message : String(err);
			}
		} finally {
			if (mine === latest) {
				loading = false;
			}
		}
	}

	$effect(() => {
		void load();
		return () => {
			// A load that finishes after the tab closed has nowhere to go.
			latest++;
		};
	});

	const text = $derived(loaded ? JSON.stringify(doc, null, 2) : '');
	const searching = $derived(query.trim() !== '');
	const found = $derived(searching ? searchJson(doc, query, maxHits) : null);
	// Pieces of a large document are only cut when the document is shown.
	const tokens = $derived(loaded && !searching ? jsonTokens(text) : []);
</script>

<Card title="docker inspect" description="The container's whole inspection, as Docker writes it.">
	{#snippet actions()}
		<IconButton label="Refresh" disabled={loading} onclick={() => void load()}>
			<RefreshCw class={['size-3.5', loading && 'animate-spin']} />
		</IconButton>
		{#if text}
			<CopyButton value={text} label="Copy JSON" />
		{/if}
	{/snippet}
	{#if !loaded}
		{#if error}
			<EmptyState title="Couldn't load the inspection">
				<span class="font-mono text-[12px] break-words text-red-fg">{error}</span>
				<div class="mt-3">
					<Button {loading} onclick={() => void load()}>Retry</Button>
				</div>
			</EmptyState>
		{:else}
			<div class="p-4"><Skeleton class="h-64" /></div>
		{/if}
	{:else}
		{#if error}
			<p
				role="alert"
				class="border-b border-amber/25 bg-amber/5 px-4 py-2 text-[13px] break-words text-amber-fg"
			>
				Refresh failed: {error}
			</p>
		{/if}
		<div class="relative border-b border-line p-3">
			<Search
				class="pointer-events-none absolute top-1/2 left-5.5 size-3.5 -translate-y-1/2 text-fg-faint"
			/>
			<Input
				type="text"
				bind:value={query}
				placeholder="Search keys and values"
				aria-label="Search the inspection"
				autocomplete="off"
				spellcheck="false"
				class="h-8 pl-8 text-[13px]"
			/>
		</div>
		<div class="max-h-[calc(100dvh-22rem)] min-h-80 overflow-auto bg-canvas">
			{#if found}
				{#if found.total === 0}
					<p class="px-4 py-6 text-center text-[13px] text-fg-subtle">Nothing matches “{query}”.</p>
				{:else}
					<p class="border-b border-line px-4 py-2 text-[12px] text-fg-subtle" role="status">
						{plural(found.total, 'match', 'matches')}{found.total > found.hits.length
							? `, the first ${found.hits.length} shown`
							: ''}
					</p>
					<ul>
						{#each found.hits as hit, i (i)}
							<li class="border-b border-line px-4 py-2 last:border-0">
								<div class="font-mono text-[11.5px] break-all text-fg-subtle">{hit.path}</div>
								<div class="font-mono text-[12.5px] break-all text-fg">{hit.text}</div>
							</li>
						{/each}
					</ul>
				{/if}
			{:else}
				<code
					class="block w-max min-w-full p-4 font-mono text-[12px] leading-[1.6] whitespace-pre text-fg-muted"
					>{#each tokens as t, i (i)}<span class={kindClass[t.kind]}>{t.text}</span>{/each}</code
				>
			{/if}
		</div>
	{/if}
</Card>
