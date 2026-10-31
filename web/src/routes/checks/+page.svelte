<script lang="ts">
	import HeartPulse from '@lucide/svelte/icons/heart-pulse';
	import Plus from '@lucide/svelte/icons/plus';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { deleteCheck } from '#lib/api/client.js';
	import type { Check } from '#lib/api/types.js';
	import { groupChecks, matches, parseFilter, type CheckFilter } from '#lib/checks.js';
	import CheckDetail from '#lib/checks/CheckDetail.svelte';
	import CheckDialog from '#lib/checks/CheckDialog.svelte';
	import CheckRow from '#lib/checks/CheckRow.svelte';
	import { RecentResults } from '#lib/checks/recent.svelte.js';
	import { live } from '#lib/live.svelte.js';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	const checks = $derived(live.snapshot?.checks ?? []);

	// The latest results of each check, for its bars: loaded when the page
	// opens, and again after the stream reconnects, since results came while
	// it was down. Each snapshot adds the newest.
	const recent = new RecentResults();
	$effect(() => {
		if (live.connection === 'live') {
			recent.load(() => live.snapshot?.checks ?? []);
		}
	});
	$effect(() => {
		recent.observe(checks);
	});
	const health = $derived(live.snapshot?.health);
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());

	// The filter and the open check are in the URL, so links can point at
	// them, such as the health card's to the failing checks.
	const filter = $derived(parseFilter(page.url.searchParams.get('state')));
	const openId = $derived(page.url.searchParams.get('check'));
	const opened = $derived(checks.find((c) => c.id === openId));

	const groups = $derived(groupChecks(checks.filter((c) => matches(c, filter))));
	const groupNames = $derived([...new Set(checks.filter((c) => !c.deviceId).map((c) => c.group))]);
	const filters = $derived<{ value: CheckFilter; label: string }[]>([
		{ value: 'all', label: `All ${health?.total ?? ''}` },
		{ value: 'passing', label: `Passing ${health?.passing ?? ''}` },
		{ value: 'failing', label: `Failing ${health?.failing ?? ''}` },
		{ value: 'unavailable', label: `Unavailable ${health?.unavailable ?? ''}` }
	]);

	/** Sets a query parameter, or takes it away if value is null. */
	function setParam(key: string, value: string | null) {
		const url = new URL(page.url.href);
		if (value === null) {
			url.searchParams.delete(key);
		} else {
			url.searchParams.set(key, value);
		}
		goto(url, { replace: true, reset: false });
	}

	/** The check being added (null) or changed, while the dialog is open. */
	let editing = $state<Check | null>(null);
	let editOpen = $state(false);
	let removing = $state<Check | null>(null);
	let removeOpen = $state(false);
	let busy = $state(false);

	function edit(c: Check | null) {
		editing = c;
		editOpen = true;
	}

	function askRemove(c: Check) {
		removing = c;
		removeOpen = true;
	}

	async function remove() {
		if (!removing) {
			return;
		}
		busy = true;
		try {
			await deleteCheck(removing.id);
			removeOpen = false;
		} catch {
			// deleteCheck showed the error as a toast.
		} finally {
			busy = false;
		}
	}
</script>

{#snippet plus()}
	<Plus class="size-3.5" />
{/snippet}

<PageHeader
	title="Checks"
	description="HTTP, TCP and DNS checks run from the hub, and a ping of every machine on the tailnet. The ones that run make up the health score."
>
	{#snippet actions()}
		<AdminOnly>
			<Button variant="primary" icon={plus} onclick={() => edit(null)}>New check</Button>
		</AdminOnly>
	{/snippet}
</PageHeader>

<div class="mb-4">
	<Segmented
		bind:value={() => filter, (v) => setParam('state', v === 'all' ? null : v)}
		options={filters}
		label="Show checks"
	/>
</div>

{#if !live.snapshot}
	<Card>
		{#each [1, 2, 3] as i (i)}
			<div class="flex items-center gap-3 border-b border-line px-4 py-3 last:border-0">
				<Skeleton class="size-2 rounded-full" />
				<div class="flex-1 space-y-1.5">
					<Skeleton class="h-4 w-40" />
					<Skeleton class="h-3 w-56" />
				</div>
				<Skeleton class="hidden h-5 w-56 sm:block" />
			</div>
		{/each}
	</Card>
{:else if groups.length === 0}
	<Card>
		{#if checks.length === 0}
			<EmptyState title="No checks yet" icon={HeartPulse}>
				Add a check of a website, a port or a host name, and Roost runs it from the hub and alerts
				you when it fails.
				<AdminOnly>
					<div class="mt-3">
						<Button variant="primary" icon={plus} onclick={() => edit(null)}>Add a check</Button>
					</div>
				</AdminOnly>
			</EmptyState>
		{:else}
			<EmptyState title="No checks match" icon={HeartPulse}>
				None of the checks is {filter} now.
			</EmptyState>
		{/if}
	</Card>
{:else}
	<div class="space-y-4">
		{#each groups as g (g.name)}
			<Card title={g.name}>
				{#snippet actions()}
					<span class="num text-[12px] text-fg-subtle">
						{g.checks.filter((c) => c.state === 'passing').length}/{g.checks.length} passing
					</span>
				{/snippet}
				<ul class="divide-y divide-line">
					{#each g.checks as c (c.id)}
						<CheckRow
							check={c}
							recent={recent.byId[c.id] ?? []}
							{now}
							onopen={() => setParam('check', c.id)}
							onedit={() => edit(c)}
							ondelete={() => askRemove(c)}
						/>
					{/each}
				</ul>
			</Card>
		{/each}
	</div>
{/if}

{#if opened}
	<CheckDetail
		check={opened}
		recent={recent.byId[opened.id] ?? []}
		bind:open={() => true, (v) => !v && setParam('check', null)}
	/>
{/if}

<CheckDialog check={editing} bind:open={editOpen} groups={groupNames} />

<Dialog
	bind:open={removeOpen}
	title="Delete {removing?.name ?? 'this check'}?"
	description="Roost stops running it, and forgets its history."
>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (removeOpen = false)}>Cancel</Button>
		<Button variant="danger" loading={busy} onclick={remove}>Delete</Button>
	{/snippet}
	<p class="text-[13px] text-fg-muted">The health score leaves it out from then on.</p>
</Dialog>
