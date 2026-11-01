<script lang="ts">
	import BellOff from '@lucide/svelte/icons/bell-off';
	import History from '@lucide/svelte/icons/history';
	import ShieldCheck from '@lucide/svelte/icons/shield-check';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { activeAlerts, mergeById, parseAlertTab, type AlertTab } from '#lib/alerts.js';
	import AlertCard from '#lib/alerts/AlertCard.svelte';
	import NotificationsHint from '#lib/alerts/NotificationsHint.svelte';
	import { ApiError, getAlert, getResolvedAlerts } from '#lib/api/client.js';
	import type { Alert } from '#lib/api/types.js';
	import { live } from '#lib/live.svelte.js';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import TabPanel from '#lib/ui/TabPanel.svelte';
	import Tabs from '#lib/ui/Tabs.svelte';
	import ErrorState from '#lib/ui/ErrorState.svelte';

	const alerts = $derived(live.snapshot?.alerts ?? []);
	const devices = $derived(live.snapshot?.devices ?? []);
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());
	const open = $derived(activeAlerts(alerts));
	const snoozed = $derived(alerts.filter((a) => a.state === 'snoozed'));

	// The tab is in the URL, so links can point at it.
	const tab = $derived(parseAlertTab(page.url.searchParams.get('tab')));

	function setTab(t: string) {
		const url = new URL(page.url.href);
		if (t === 'open') {
			url.searchParams.delete('tab');
		} else {
			url.searchParams.set('tab', t);
		}
		goto(url, { replace: true, reset: false });
	}

	// A link in a notification points at one alert, with ?alert=12. It's shown
	// above the tabs: the live alert while it's open, and then the one the hub
	// has on record, which is also there once it resolved.
	const focusId = $derived.by(() => {
		const n = Number(page.url.searchParams.get('alert'));
		return Number.isSafeInteger(n) && n > 0 ? n : null;
	});
	const focusLive = $derived(focusId === null ? undefined : alerts.find((a) => a.id === focusId));
	let focusFetched = $state.raw<Alert | null>(null);
	let focusStatus = $state<'loading' | 'gone' | 'failed'>('loading');
	const focus = $derived(focusLive ?? focusFetched);

	$effect(() => {
		if (focusId === null || focusLive) {
			return;
		}
		let current = true;
		focusFetched = null;
		focusStatus = 'loading';
		getAlert(focusId)
			.then((a) => {
				if (current) {
					focusFetched = a;
				}
			})
			.catch((err) => {
				if (current) {
					focusStatus = err instanceof ApiError && err.status === 404 ? 'gone' : 'failed';
				}
			});
		return () => {
			current = false;
		};
	});

	function clearFocus() {
		const url = new URL(page.url.href);
		url.searchParams.delete('alert');
		goto(url, { replace: true, reset: false });
	}

	// Resolved alerts aren't in the snapshots, so the tab loads them, and
	// again when one resolves.
	let resolved = $state.raw<Alert[] | null>(null);
	let more = $state(false);
	let loading = $state(false);
	const openIds = $derived(alerts.map((a) => a.id).join());

	$effect(() => {
		if (tab !== 'resolved') {
			return;
		}
		void openIds;
		let current = true;
		getResolvedAlerts()
			.then((list) => {
				if (!current) {
					return;
				}
				if (resolved === null) {
					resolved = list.alerts;
					more = list.more;
					return;
				}
				// The first page joins the alerts loaded, so older ones stay
				// where they are.
				resolved = mergeById(resolved, list.alerts);
				more ||= list.more;
			})
			.catch(() => {
				if (current) {
					resolved ??= [];
				}
			});
		return () => {
			current = false;
		};
	});

	async function loadOlder() {
		const last = resolved?.at(-1);
		if (!last) {
			return;
		}
		loading = true;
		try {
			const list = await getResolvedAlerts(last.id);
			resolved = [...(resolved ?? []), ...list.alerts];
			more = list.more;
		} catch {
			// The button stays, to try again.
		} finally {
			loading = false;
		}
	}

	function deviceOf(a: Alert) {
		return devices.find((d) => d.id === a.deviceId);
	}

	const tabs = $derived([
		{ value: 'open', label: 'Open', count: open.length },
		{ value: 'snoozed', label: 'Snoozed', count: snoozed.length },
		{ value: 'resolved', label: 'Resolved' }
	]);
</script>

{#snippet list(items: Alert[])}
	<div class="grid gap-2 p-3 sm:p-4">
		{#each items as a (a.id)}
			<AlertCard alert={a} device={deviceOf(a)} {now} />
		{/each}
	</div>
{/snippet}

<PageHeader
	title="Alerts"
	description="Problems Bield found on devices, checks, containers and services. They resolve when the problem clears."
/>

{#if focusId !== null}
	<Card title="Alert {focusId}" class="mb-4">
		{#snippet actions()}
			<Button size="xs" variant="ghost" onclick={clearFocus}>Show all alerts</Button>
		{/snippet}
		{#if focus}
			<div class="p-3 sm:p-4">
				<AlertCard alert={focus} device={deviceOf(focus)} {now} />
			</div>
		{:else if focusStatus === 'loading'}
			<div class="p-4"><Skeleton class="h-20" /></div>
		{:else if focusStatus === 'gone'}
			<EmptyState title="This alert isn't here">
				Alerts that resolved are kept for 30 days, so this one may be older.
			</EmptyState>
		{:else}
			<ErrorState title="Couldn't load this alert" message="Try again in a moment." />
		{/if}
	</Card>
{/if}

<Card>
	<div class="px-2">
		<Tabs bind:value={() => tab, setTab} {tabs}>
			<TabPanel value="open">
				{#if !live.snapshot}
					<div class="space-y-2 p-4">
						<Skeleton class="h-20" />
						<Skeleton class="h-20" />
					</div>
				{:else if open.length === 0}
					<EmptyState title="No open alerts" icon={ShieldCheck}>
						Everything the hub can see looks fine. Bield opens an alert when a machine goes offline,
						a disk fills up, a check keeps failing or a service stops.
						<AdminOnly><NotificationsHint /></AdminOnly>
					</EmptyState>
				{:else}
					{@render list(open)}
				{/if}
			</TabPanel>
			<TabPanel value="snoozed">
				{#if snoozed.length === 0}
					<EmptyState title="Nothing snoozed" icon={BellOff}>
						A snoozed alert waits here until it wakes, or resolves.
					</EmptyState>
				{:else}
					{@render list(snoozed)}
				{/if}
			</TabPanel>
			<TabPanel value="resolved">
				{#if resolved === null}
					<div class="space-y-2 p-4">
						<Skeleton class="h-20" />
						<Skeleton class="h-20" />
					</div>
				{:else if resolved.length === 0}
					<EmptyState title="No resolved alerts yet" icon={History}>
						Alerts that cleared stay here for 30 days.
					</EmptyState>
				{:else}
					{@render list(resolved)}
					{#if more}
						<div class="flex justify-center pb-4">
							<Button onclick={loadOlder} {loading}>Load older</Button>
						</div>
					{/if}
				{/if}
			</TabPanel>
		</Tabs>
	</div>
</Card>
