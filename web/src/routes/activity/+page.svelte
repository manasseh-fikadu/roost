<script lang="ts">
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { isAdmin } from '#lib/access.js';
	import { eventKindLabels, eventLevelStatus, mergeById } from '#lib/alerts.js';
	import { getEvents } from '#lib/api/client.js';
	import type { CollectorState, Event, EventKind } from '#lib/api/types.js';
	import { ago, dateTime } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import type { Status } from '#lib/status.js';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	type Filter = EventKind | 'all';

	const admin = $derived(isAdmin(page.data.me));
	const devices = $derived(live.snapshot?.devices ?? []);
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());
	const lastEventId = $derived(live.snapshot?.lastEventId ?? 0);

	// Who has a role is for admins, so viewers don't get those events.
	const kinds = $derived(
		(Object.keys(eventKindLabels) as EventKind[]).filter((k) => admin || k !== 'access')
	);
	const filters = $derived<{ value: Filter; label: string }[]>([
		{ value: 'all', label: 'All' },
		...kinds.map((k) => ({ value: k, label: eventKindLabels[k] }))
	]);
	const kind = $derived.by((): Filter => {
		const k = page.url.searchParams.get('kind');
		return kinds.find((x) => x === k) ?? 'all';
	});

	function setKind(k: Filter) {
		const url = new URL(page.url.href);
		if (k === 'all') {
			url.searchParams.delete('kind');
		} else {
			url.searchParams.set('kind', k);
		}
		goto(url, { replace: true, reset: false });
	}

	let events = $state.raw<Event[] | null>(null);
	let more = $state(false);
	let loading = $state(false);
	let failed = $state(false);

	function query(k: Filter) {
		return k === 'all' ? {} : { kind: k };
	}

	// The first page loads for each filter.
	$effect(() => {
		const k = kind;
		let current = true;
		events = null;
		failed = false;
		getEvents(query(k))
			.then((list) => {
				if (current) {
					events = list.events;
					more = list.more;
				}
			})
			.catch(() => {
				if (current) {
					failed = true;
				}
			});
		return () => {
			current = false;
		};
	});

	// Newer events load as the snapshots say there are some.
	$effect(() => {
		const newest = events?.[0]?.id;
		const k = kind;
		if (newest === undefined || lastEventId <= newest) {
			return;
		}
		let current = true;
		getEvents({ ...query(k), after: newest })
			.then((list) => {
				if (!current || !events) {
					return;
				}
				// Over a page of newer events are the first page, and there are
				// more since the ones loaded, so they start the list again.
				if (list.more) {
					events = list.events;
					more = true;
				} else {
					events = mergeById(events, list.events);
				}
			})
			.catch(() => {
				// The next snapshot tries again.
			});
		return () => {
			current = false;
		};
	});

	async function loadOlder() {
		const last = events?.at(-1);
		if (!last) {
			return;
		}
		loading = true;
		try {
			const list = await getEvents({ ...query(kind), before: last.id });
			events = [...(events ?? []), ...list.events];
			more = list.more;
		} catch {
			// The button stays, to try again.
		} finally {
			loading = false;
		}
	}

	function deviceName(id: string | undefined): string | undefined {
		return id ? devices.find((d) => d.id === id)?.name : undefined;
	}

	const collectorStatus: Record<CollectorState, Status> = {
		ok: 'online',
		failing: 'offline',
		waiting: 'unknown'
	};
</script>

<PageHeader
	title="Activity"
	description="What Roost saw happen, such as devices coming and going and alerts opening and resolving, and what anyone did in Roost."
/>

<div class="grid grid-cols-[minmax(0,1fr)] gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
	<div class="min-w-0">
		<div class="mb-3 overflow-x-auto">
			<Segmented bind:value={() => kind, setKind} options={filters} label="Show events" />
		</div>
		<Card>
			{#if failed}
				<p class="px-4 py-10 text-center text-[13px] text-fg-subtle">
					Couldn't load the activity log. Try again in a moment.
				</p>
			{:else if events === null}
				<div class="space-y-2 p-4">
					{#each [1, 2, 3, 4] as i (i)}
						<Skeleton class="h-9" />
					{/each}
				</div>
			{:else if events.length === 0}
				<EmptyState title="Nothing yet" icon={ActivityIcon}>
					Nothing of this kind has happened in the last 30 days.
				</EmptyState>
			{:else}
				<ul class="divide-y divide-line">
					{#each events as e (e.id)}
						{@const device = deviceName(e.deviceId)}
						<li class="flex items-start gap-3 px-4 py-2.5">
							<StatusDot status={eventLevelStatus[e.level]} class="mt-1.5" />
							<div class="min-w-0 flex-1">
								<p class="text-[13px] leading-5 break-words text-fg">{e.message}</p>
								<p class="font-mono text-[11px] text-fg-faint">
									{eventKindLabels[e.kind] ?? e.kind}
									{#if device && e.deviceId}
										·
										<a
											href={resolve('/devices/[id]', { id: e.deviceId })}
											class="text-fg-subtle hover:text-fg hover:underline">{device}</a
										>
									{/if}
									{#if e.actor}
										· by {e.actor}
									{/if}
								</p>
							</div>
							<span class="shrink-0 num text-[11px] text-fg-faint" title={dateTime(e.at)}>
								{ago(e.at, now)}
							</span>
						</li>
					{/each}
				</ul>
			{/if}
		</Card>
		{#if more && events && events.length > 0}
			<div class="mt-3 flex justify-center">
				<Button onclick={loadOlder} {loading}>Load older</Button>
			</div>
		{/if}
	</div>

	<Card title="Collectors" description="The jobs that gather what Roost knows.">
		{#each live.snapshot?.collectors ?? [] as c (c.name)}
			<div class="border-b border-line px-4 py-2.5 last:border-0">
				<div class="flex items-center justify-between gap-2">
					<span class="flex items-center gap-2 text-[13px] font-medium text-fg">
						<StatusDot status={collectorStatus[c.state]} />
						{c.name}
					</span>
					<span class="font-mono text-[11px] text-fg-subtle">{c.state}</span>
				</div>
				<p class="mt-0.5 font-mono text-[11px] text-fg-faint">
					{c.lastOk ? `worked ${ago(c.lastOk, now)}` : 'hasn’t worked yet'}
				</p>
				{#if c.error}
					<p class="mt-1 font-mono text-[11px] break-words text-red-fg/90">{c.error}</p>
				{/if}
			</div>
		{:else}
			<Skeleton class="m-4 h-16" />
		{/each}
	</Card>
</div>
