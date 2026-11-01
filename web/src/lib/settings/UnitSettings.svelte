<!--
	The systemd units on Roost's machine that Roost watches, picked from the
	units there are. The picks are a draft until Save. A unit the machine
	doesn't have can still be watched, so a list that names one stays right
	before the unit is installed.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import Plus from '@lucide/svelte/icons/plus';
	import Search from '@lucide/svelte/icons/search';
	import Server from '@lucide/svelte/icons/server';
	import X from '@lucide/svelte/icons/x';
	import { changeSettings, getInstalledServices } from '#lib/api/client.js';
	import type { InstalledServices, SettingsView } from '#lib/api/types.js';
	import { plural } from '#lib/format.js';
	import { matchServices, parseUnit, readUnitName, sameSet, unitKey } from '#lib/settings.js';
	import type { Tone } from '#lib/status.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Button from '#lib/ui/Button.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import ErrorState from '#lib/ui/ErrorState.svelte';
	import { Draft } from './draft.svelte.js';
	import FormCard from './FormCard.svelte';

	interface Props {
		view: SettingsView;
		/** Called with the settings the hub answers with after a save. */
		onsaved: (view: SettingsView) => void;
	}

	let { view, onsaved }: Props = $props();

	const id = $props.id();

	// The draft starts from the saved units, in the order they were saved. It
	// follows them if someone else changes them while nothing here is picked,
	// and stays as it is once something is.
	const draft = new Draft(() => view.settings.watchedUnits ?? [], sameSet);
	const selected = $derived(draft.value);
	let saving = $state(false);
	let query = $state('');
	let newName = $state('');
	let newScope = $state<'system' | 'user'>('system');

	const selectedSet = $derived(new Set(selected));
	const savedSet = $derived(new Set(draft.base));

	// The installed services, which are loaded once.
	let installed = $state.raw<InstalledServices | null>(null);
	let loadError = $state<string | null>(null);
	let loading = $state(true);
	/** Counts the loads, so only the latest one counts. */
	let latest = 0;

	async function load() {
		const mine = ++latest;
		loading = true;
		try {
			const result = await getInstalledServices();
			if (mine === latest) {
				installed = result;
				loadError = null;
			}
		} catch (err) {
			if (mine === latest) {
				loadError = err instanceof Error ? err.message : String(err);
			}
		} finally {
			if (mine === latest) {
				loading = false;
			}
		}
	}

	onMount(() => {
		void load();
		return () => {
			// A load that finishes after the page was left has nowhere to go.
			latest++;
		};
	});

	const waiting = $derived(!installed && !loadError);
	const ready = $derived(installed?.available === true);
	const services = $derived(installed?.services ?? []);
	const installedKeys = $derived(new Set(services.map((s) => s.key)));
	// The watched units come first, as they were saved, so that ticking a
	// box doesn't move its row under the pointer.
	const ordered = $derived(
		[...services].sort((a, b) => Number(savedSet.has(b.key)) - Number(savedSet.has(a.key)))
	);
	const shown = $derived(matchServices(ordered, query));
	// The units picked that the machine doesn't have, or that can't be
	// checked while the list isn't there.
	const others = $derived(waiting ? [] : selected.filter((k) => !installedKeys.has(k)));

	const parsed = $derived(readUnitName(newName));

	function toggle(key: string) {
		draft.value = selectedSet.has(key) ? selected.filter((k) => k !== key) : [...selected, key];
	}

	function remove(key: string) {
		draft.value = selected.filter((k) => k !== key);
	}

	function add() {
		if (parsed.name === '' || parsed.problem) {
			return;
		}
		const key = unitKey(parsed.name, newScope === 'user');
		if (!selectedSet.has(key)) {
			draft.value = [...selected, key];
		}
		newName = '';
	}

	async function save() {
		saving = true;
		try {
			const next = await changeSettings({ watchedUnits: [...selected] }, 'Watched units saved');
			draft.adopt(next.settings.watchedUnits ?? []);
			onsaved(next);
		} catch {
			// changeSettings showed the error.
		} finally {
			saving = false;
		}
	}

	/** Enter in these fields shouldn't save the form: it searches, or adds. */
	function keepForm(e: KeyboardEvent, then?: () => void) {
		if (e.key === 'Enter') {
			e.preventDefault();
			then?.();
		}
	}

	function stateOf(active: string | undefined): { label: string; tone: Tone } {
		switch (active) {
			case undefined:
			case '':
				return { label: 'installed', tone: 'gray' };
			case 'active':
				return { label: 'active', tone: 'green' };
			case 'failed':
				return { label: 'failed', tone: 'red' };
			default:
				return { label: active, tone: 'gray' };
		}
	}
</script>

<FormCard
	title="Watched units"
	description="Roost shows these units on the hub's page and alerts when one stops. Failed units always show."
	dirty={draft.dirty}
	{saving}
	onsave={save}
	onreset={() => draft.reset()}
>
	{#if waiting}
		<div class="space-y-2" aria-busy="true">
			<Skeleton class="h-9" />
			<Skeleton class="h-48" />
		</div>
	{:else if !installed}
		<div class="rounded-lg border border-line">
			<ErrorState
				title="Couldn't load the services"
				message={loadError ?? undefined}
				{loading}
				onretry={() => void load()}
			/>
		</div>
	{:else if !installed.available}
		<div class="rounded-lg border border-line">
			<EmptyState title="Can't read systemd on this machine" icon={Server}>
				It needs systemd to list the units there are, so it can't offer them here.
			</EmptyState>
		</div>
	{:else}
		<div class="space-y-2">
			<div class="relative">
				<Search
					class="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-fg-subtle"
				/>
				<Input
					bind:value={query}
					type="search"
					class="pl-9"
					placeholder="Search services"
					aria-label="Search services"
					autocomplete="off"
					spellcheck={false}
					onkeydown={keepForm}
				/>
			</div>
			<div class="max-h-96 overflow-y-auto rounded-lg border border-line">
				{#if shown.length === 0}
					<p class="px-4 py-8 text-center text-[13px] text-fg-subtle">
						{services.length === 0
							? 'systemd has no services on this machine.'
							: 'No service matches the search.'}
					</p>
				{:else}
					<ul class="divide-y divide-line">
						{#each shown as s (s.key)}
							{@const badge = stateOf(s.active)}
							{@const checked = selectedSet.has(s.key)}
							<li>
								<label
									class={[
										'flex cursor-pointer items-center gap-3 px-3 py-2 transition-colors hover:bg-hover',
										checked && 'bg-hover/60'
									]}
								>
									<input
										type="checkbox"
										class="size-4 shrink-0 cursor-pointer accent-blue"
										{checked}
										onchange={() => toggle(s.key)}
									/>
									<span class="flex min-w-0 flex-1 flex-col sm:flex-row sm:items-baseline sm:gap-3">
										<span class="truncate font-mono text-[13px] text-fg sm:max-w-[55%] sm:shrink-0">
											{s.name}
										</span>
										{#if s.description}
											<span class="truncate text-[12.5px] text-fg-subtle">{s.description}</span>
										{/if}
									</span>
									{#if s.scope === 'user'}
										<Badge tone="purple">user</Badge>
									{/if}
									<Badge tone={badge.tone}>{badge.label}</Badge>
								</label>
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		</div>
	{/if}

	{#if others.length > 0}
		<div>
			<h3 class="mb-2 text-[13px] font-medium text-fg-muted">
				{ready ? 'Not installed on this machine' : 'Currently watched'}
			</h3>
			<ul class="divide-y divide-line rounded-lg border border-line">
				{#each others as key (key)}
					{@const unit = parseUnit(key)}
					<li class="flex items-center gap-3 py-1.5 pr-2 pl-3">
						<span class="min-w-0 flex-1 truncate font-mono text-[13px] text-fg">{unit.name}</span>
						{#if unit.user}
							<Badge tone="purple">user</Badge>
						{/if}
						<IconButton label="Remove {unit.name}" size="sm" onclick={() => remove(key)}>
							<X class="size-4" />
						</IconButton>
					</li>
				{/each}
			</ul>
		</div>
	{/if}

	{#if ready}
		<div>
			<label class="mb-2 block text-[13px] font-medium text-fg-muted" for="{id}-name">
				Add by name
			</label>
			<div class="flex flex-col gap-2 sm:flex-row">
				<div class="min-w-0 flex-1">
					<Input
						id="{id}-name"
						bind:value={newName}
						class="font-mono text-[13px]"
						placeholder="nginx.service"
						autocomplete="off"
						autocapitalize="off"
						spellcheck={false}
						onkeydown={(e) => keepForm(e, add)}
					/>
				</div>
				<div class="sm:w-28">
					<Select bind:value={newScope} aria-label="Whose unit it is">
						<option value="system">System</option>
						<option value="user">User</option>
					</Select>
				</div>
				<Button
					size="md"
					class="h-9"
					disabled={parsed.name === '' || parsed.problem !== ''}
					onclick={add}
				>
					{#snippet icon()}
						<Plus class="size-4" />
					{/snippet}
					Add
				</Button>
			</div>
			<p class={['mt-1.5 text-[12px]', parsed.problem ? 'text-red-fg' : 'text-fg-subtle']}>
				{parsed.problem ||
					'For a unit that isn’t installed yet. A name without a dot gets .service after it.'}
			</p>
		</div>
	{/if}

	{#snippet note()}
		{plural(selected.length, 'unit')} watched
	{/snippet}
</FormCard>
