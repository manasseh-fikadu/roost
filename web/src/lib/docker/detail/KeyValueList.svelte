<!--
	A filterable list of names and values, such as a container's environment.
	A value marked `secret` shows as dots until someone reveals it, one at a
	time or all at once. Copying a value copies the real one.
-->
<script lang="ts">
	import Eye from '@lucide/svelte/icons/eye';
	import EyeOff from '@lucide/svelte/icons/eye-off';
	import Search from '@lucide/svelte/icons/search';
	import { SvelteSet } from 'svelte/reactivity';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import CopyButton from '#lib/ui/CopyButton.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Input from '#lib/ui/Input.svelte';
	import { filterRows, type KeyRow } from './helpers.js';

	interface Props {
		title: string;
		description?: string;
		/** The name of one row, for the filter box and the buttons: "variable", "label". */
		noun: string;
		rows: KeyRow[];
		/** What to say when there are no rows. */
		empty: string;
	}

	let { title, description, noun, rows, empty }: Props = $props();

	let query = $state('');
	let revealAll = $state(false);
	const revealed = new SvelteSet<string>();

	const shown = $derived(filterRows(rows, query));
	const anySecret = $derived(rows.some((r) => r.secret));

	function toggle(id: string) {
		if (!revealed.delete(id)) {
			revealed.add(id);
		}
	}

	function toggleAll() {
		revealAll = !revealAll;
		revealed.clear();
	}
</script>

{#if rows.length === 0}
	<Card>
		<EmptyState title={empty} />
	</Card>
{:else}
	<Card {title} {description}>
		{#snippet actions()}
			{#if anySecret}
				<Button size="sm" variant="ghost" onclick={toggleAll} aria-pressed={revealAll}>
					{#snippet icon()}
						{#if revealAll}<EyeOff class="size-3.5" />{:else}<Eye class="size-3.5" />{/if}
					{/snippet}
					{revealAll ? 'Hide values' : 'Reveal all'}
				</Button>
			{/if}
		{/snippet}
		<div class="relative border-b border-line p-3">
			<Search
				class="pointer-events-none absolute top-1/2 left-5.5 size-3.5 -translate-y-1/2 text-fg-faint"
			/>
			<Input
				type="text"
				bind:value={query}
				placeholder="Filter {noun}s"
				aria-label="Filter {noun}s"
				autocomplete="off"
				spellcheck="false"
				class="h-8 pl-8 text-[13px]"
			/>
		</div>
		{#if shown.length === 0}
			<p class="px-4 py-6 text-center text-[13px] text-fg-subtle">Nothing matches “{query}”.</p>
		{:else}
			<ul>
				{#each shown as r (r.id)}
					{@const hidden = r.secret && !revealAll && !revealed.has(r.id)}
					<li
						class="flex flex-col gap-0.5 border-b border-line px-4 py-2 last:border-0 sm:flex-row sm:items-start sm:gap-4"
					>
						<span
							class="font-mono text-[12.5px] break-all text-fg-muted sm:w-2/5 sm:max-w-80 sm:shrink-0"
						>
							{r.key}
						</span>
						<div class="flex min-w-0 flex-1 items-start gap-1">
							<span class="min-w-0 flex-1 font-mono text-[12.5px] break-all text-fg">
								{#if hidden}
									<span class="text-fg-subtle select-none" aria-label="Hidden value">••••••••</span>
								{:else if r.value === ''}
									<span class="text-fg-faint">empty</span>
								{:else}
									{r.value}
								{/if}
							</span>
							<span class="-my-1 flex shrink-0 items-center">
								{#if r.secret}
									<IconButton
										size="sm"
										label={hidden ? `Reveal ${r.key}` : `Hide ${r.key}`}
										aria-pressed={!hidden}
										disabled={revealAll}
										onclick={() => toggle(r.id)}
									>
										{#if hidden}<Eye class="size-3.5" />{:else}<EyeOff class="size-3.5" />{/if}
									</IconButton>
								{/if}
								<CopyButton value={r.value} label="Copy {r.key}" />
							</span>
						</div>
					</li>
				{/each}
			</ul>
		{/if}
	</Card>
{/if}
