<!--
	Settings: how Roost looks and behaves, and who can use it. Admins only.
	The tab is in the URL, /settings?tab=alerts, so links can point at it. Each
	tab saves its own part of the settings, and Roost uses a change at once.
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { getSettings } from '#lib/api/client.js';
	import type { SettingsView } from '#lib/api/types.js';
	import AccessSettings from '#lib/settings/AccessSettings.svelte';
	import AlertSettings from '#lib/settings/AlertSettings.svelte';
	import GeneralSettings from '#lib/settings/GeneralSettings.svelte';
	import NotificationSettings from '#lib/settings/NotificationSettings.svelte';
	import RetentionSettings from '#lib/settings/RetentionSettings.svelte';
	import SetupCard from '#lib/settings/SetupCard.svelte';
	import UnitSettings from '#lib/settings/UnitSettings.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import TabPanel from '#lib/ui/TabPanel.svelte';
	import Tabs from '#lib/ui/Tabs.svelte';

	const tabs = [
		{ value: 'general', label: 'General' },
		{ value: 'alerts', label: 'Alerts' },
		{ value: 'notifications', label: 'Notifications' },
		{ value: 'units', label: 'Watched units' },
		{ value: 'retention', label: 'Retention' },
		{ value: 'access', label: 'Access' }
	];

	const tab = $derived.by(() => {
		const t = page.url.searchParams.get('tab');
		return tabs.some((x) => x.value === t) ? (t as string) : 'general';
	});

	function setTab(t: string) {
		const url = new URL(page.url.href);
		if (t === 'general') {
			url.searchParams.delete('tab');
		} else {
			url.searchParams.set('tab', t);
		}
		goto(url, { replace: true, reset: false });
	}

	let view = $state.raw<SettingsView | null>(null);
	let failure = $state('');

	async function load() {
		failure = '';
		try {
			view = await getSettings();
		} catch (err) {
			failure = err instanceof Error ? err.message : String(err);
		}
	}

	$effect(() => {
		void load();
	});

	function saved(v: SettingsView) {
		view = v;
	}
</script>

<PageHeader title="Settings" description="How Roost behaves, and who can use it." />

{#if view}
	<Tabs bind:value={() => tab, setTab} {tabs}>
		<TabPanel value="general">
			<div class="space-y-4">
				<GeneralSettings {view} onsaved={saved} />
				<SetupCard />
			</div>
		</TabPanel>
		<TabPanel value="alerts"><AlertSettings {view} onsaved={saved} /></TabPanel>
		<TabPanel value="notifications">
			<NotificationSettings {view} onsaved={saved} active={tab === 'notifications'} />
		</TabPanel>
		<TabPanel value="units"><UnitSettings {view} onsaved={saved} /></TabPanel>
		<TabPanel value="retention"><RetentionSettings {view} onsaved={saved} /></TabPanel>
		<TabPanel value="access"><AccessSettings {view} /></TabPanel>
	</Tabs>
{:else if failure}
	<Card>
		<EmptyState title="Roost couldn't load its settings">
			{failure}
			<div class="mt-3">
				<Button onclick={load}>Try again</Button>
			</div>
		</EmptyState>
	</Card>
{:else}
	<div class="space-y-3" aria-busy="true">
		<Skeleton class="h-10 w-full" />
		<Skeleton class="h-64 w-full" />
	</div>
{/if}
