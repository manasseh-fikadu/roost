<!--
	What's using a device's CPU and memory, by service or by process: live,
	or in a past minute that the history below it, or the device's history,
	picked. Command lines are only shown to admins.
-->
<script lang="ts">
	import History from '@lucide/svelte/icons/history';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import { page } from '$app/state';
	import { isAdmin } from '#lib/access.js';
	import { getDeviceUsage, getUsageHistory } from '#lib/api/client.js';
	import type { Device, DeviceUsage, UsageGroup, UsageHistory, UsageProc } from '#lib/api/types.js';
	import { ago, bytes, plural } from '#lib/format.js';
	import type { HistoryRange } from '#lib/history.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import { cpuText, groupName, kindLabel, kindTone, moment, type UsageMetric } from '#lib/usage.js';
	import UsageTrends from './UsageTrends.svelte';

	interface Props {
		device: Device;
		open: boolean;
		/** What to rank by. */
		metric: UsageMetric;
		/** The moment to show, in Unix milliseconds, or null for now. */
		at: number | null;
		/** How far back the history goes. */
		range: HistoryRange;
	}

	let {
		device,
		open = $bindable(),
		metric = $bindable(),
		at = $bindable(),
		range = $bindable()
	}: Props = $props();

	type View = 'groups' | 'procs';

	/** How many rows show before "Show all". */
	const collapsed = 12;

	let view: View = $state('groups');
	let data = $state<DeviceUsage | null>(null);
	let failed = $state('');
	let history = $state<UsageHistory | null>(null);
	let showAll = $state(false);
	/** The process whose whole command line shows. */
	let expanded = $state<number | null>(null);
	let now = $state(Date.now());
	let top: HTMLElement | undefined = $state();

	// Every snapshot passes in a new `device`, so the effects below read only
	// its ID, and don't start again on each one.
	const deviceId = $derived(device.id);

	function message(err: unknown): string {
		return err instanceof Error ? err.message : String(err);
	}

	// The sample: live, loading again as often as the device sends one, or
	// the stored minute nearest `at`.
	$effect(() => {
		if (!open) {
			return;
		}
		const id = deviceId;
		const when = at;
		let current = true;
		let timer: ReturnType<typeof setTimeout> | undefined;
		data = null;
		failed = '';
		async function load() {
			try {
				const d = await getDeviceUsage(id, when ?? undefined);
				if (!current) {
					return;
				}
				data = d;
				failed = '';
				if (when === null) {
					timer = setTimeout(load, Math.max(5, d.interval) * 1000);
				}
			} catch (err) {
				if (!current) {
					return;
				}
				failed = message(err);
				if (when === null) {
					timer = setTimeout(load, 15_000);
				}
			}
		}
		load();
		return () => {
			current = false;
			clearTimeout(timer);
		};
	});

	$effect(() => {
		if (!open) {
			return;
		}
		const id = deviceId;
		const r = range;
		let current = true;
		history = null;
		async function load() {
			try {
				const h = await getUsageHistory(id, r);
				if (current) {
					history = h;
				}
			} catch {
				// The chart stays as it was, and the next minute tries again.
			}
		}
		load();
		const timer = setInterval(load, 60_000);
		return () => {
			current = false;
			clearInterval(timer);
		};
	});

	$effect(() => {
		if (!open) {
			return;
		}
		const timer = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(timer);
	});

	const sample = $derived(data?.sample ?? null);
	const groupNames = $derived(new Map(sample?.groups.map((g) => [g.key, g.name]) ?? []));

	function byMetric<T extends { cpu: number; mem: number }>(items: T[]): T[] {
		const other: UsageMetric = metric === 'cpu' ? 'mem' : 'cpu';
		return [...items].sort((a, b) => b[metric] - a[metric] || b[other] - a[other]);
	}

	const groups = $derived(byMetric(sample?.groups ?? []));
	const procs = $derived(byMetric(sample?.procs ?? []));
	const count = $derived(view === 'groups' ? groups.length : procs.length);
	const max = $derived((view === 'groups' ? groups[0] : procs[0])?.[metric] ?? 0);
	const shown = $derived(showAll ? Infinity : collapsed);
	const total = $derived(sample?.total);
	/**
	 * When the sample was taken, by the browser's clock, which may run a
	 * little behind the hub's.
	 */
	const sampledAt = $derived(sample ? Math.min(Date.parse(sample.at), now) : now);
	/** Whether the live sample is older than the device's interval allows. */
	const stale = $derived(
		sample !== null &&
			data !== null &&
			now - Date.parse(sample.at) > Math.max(3 * data.interval * 1000, 60_000)
	);

	function pick(ts: number) {
		at = ts;
		top?.scrollIntoView({ behavior: 'smooth', block: 'start' });
	}

	const grid =
		'grid grid-cols-[minmax(0,1fr)_56px_72px] gap-x-3 sm:grid-cols-[minmax(0,1fr)_72px_88px]';
	const barColor: Record<UsageMetric, string> = { cpu: 'bg-blue-fg', mem: 'bg-purple-fg' };
</script>

{#snippet bar(value: number)}
	<span
		class="mt-1.5 block h-1 w-full overflow-hidden rounded-full bg-line-strong/50"
		aria-hidden="true"
	>
		<span
			class={['block h-full rounded-full transition-[width] duration-500', barColor[metric]]}
			style:width="{max > 0 ? Math.max(value > 0 ? 1.5 : 0, (value / max) * 100) : 0}%"
		></span>
	</span>
{/snippet}

{#snippet values(cpu: number, mem: number)}
	<span class={['text-right num text-[12.5px]', metric === 'cpu' ? 'text-fg' : 'text-fg-subtle']}>
		{cpuText(cpu)}
	</span>
	<span class={['text-right num text-[12.5px]', metric === 'mem' ? 'text-fg' : 'text-fg-subtle']}>
		{bytes(mem)}
	</span>
{/snippet}

{#snippet sortBy(m: UsageMetric, label: string)}
	<button
		type="button"
		onclick={() => (metric = m)}
		aria-pressed={metric === m}
		class={[
			'text-right transition-colors hover:text-fg',
			metric === m ? 'text-fg-muted' : 'text-fg-faint'
		]}
	>
		{label}{metric === m ? ' ↓' : ''}
	</button>
{/snippet}

{#snippet group(g: UsageGroup)}
	<div
		class={[
			grid,
			'items-start border-b border-line px-3 py-2 last:border-0',
			g[metric] === 0 && 'opacity-55'
		]}
	>
		<div class="min-w-0">
			<div class="flex min-w-0 items-center gap-2">
				<span class="truncate text-[13px] font-medium text-fg" title={g.name}>{g.name}</span>
				<Badge tone={kindTone(g.kind)}>{kindLabel(g.kind)}</Badge>
			</div>
			{@render bar(g[metric])}
			<div class="mt-1 truncate text-[11.5px] text-fg-subtle">
				{plural(g.procs, 'process', 'processes')}{#if g.top.length}<span class="text-fg-faint">
						· {g.top.join(', ')}</span
					>{/if}
			</div>
		</div>
		{@render values(g.cpu, g.mem)}
	</div>
{/snippet}

{#snippet proc(p: UsageProc)}
	{@const whole = expanded === p.pid}
	<button
		type="button"
		onclick={() => (expanded = whole ? null : p.pid)}
		aria-expanded={p.cmd ? whole : undefined}
		class={[
			grid,
			'w-full items-start border-b border-line px-3 py-2 text-left transition-colors last:border-0 hover:bg-raised',
			p[metric] === 0 && 'opacity-55'
		]}
	>
		<span class="min-w-0">
			<span class="flex min-w-0 items-baseline gap-2">
				<span class="max-w-[60%] shrink-0 truncate text-[13px] font-medium text-fg">{p.name}</span>
				<span class="shrink-0 num text-[11px] text-fg-faint">{p.pid}</span>
				<span class="min-w-0 truncate text-[11.5px] text-fg-subtle">
					{p.user ? `${p.user} · ` : ''}{groupNames.get(p.group) ?? groupName(p.group)}
				</span>
			</span>
			{@render bar(p[metric])}
			{#if p.cmd}
				<span
					class={[
						'mt-1 block font-mono text-[11px] text-fg-subtle',
						whole ? 'break-all whitespace-pre-wrap' : 'truncate'
					]}
					title={whole ? undefined : p.cmd}
				>
					{p.cmd}
				</span>
			{/if}
		</span>
		{@render values(p.cpu, p.mem)}
	</button>
{/snippet}

<Dialog
	bind:open
	title="What's using {device.name}"
	description={metric === 'cpu' ? 'CPU by service and process' : 'Memory by service and process'}
	width="sm:max-w-3xl"
>
	<div bind:this={top} class="scroll-mt-4"></div>
	<div class="mb-3 flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
		<div class="flex flex-wrap items-center gap-2">
			<Segmented
				bind:value={metric}
				options={[
					{ value: 'cpu', label: 'CPU' },
					{ value: 'mem', label: 'Memory' }
				]}
				label="Rank by"
			/>
			{#if sample}
				<Segmented
					bind:value={
						() => view,
						(v) => {
							view = v;
							showAll = false;
						}
					}
					options={[
						{ value: 'groups' as View, label: 'Services' },
						{ value: 'procs' as View, label: 'Processes' }
					]}
					label="Show"
				/>
			{/if}
		</div>
		{#if at !== null}
			<div class="flex min-w-0 items-center gap-2">
				<span class="flex min-w-0 items-center gap-1.5 text-[13px] text-fg">
					<History class="size-3.5 shrink-0 text-fg-subtle" />
					<span class="truncate">At {moment(sample ? Date.parse(sample.at) : at, now)}</span>
				</span>
				<Button size="xs" onclick={() => (at = null)}>
					{#snippet icon()}<RefreshCw class="size-3" />{/snippet}
					Back to live
				</Button>
			</div>
		{:else if sample}
			<span
				class={[
					'flex items-center gap-1.5 text-[12px]',
					stale ? 'text-amber-fg' : 'text-fg-subtle'
				]}
			>
				<span class="relative inline-flex size-2" aria-hidden="true">
					{#if !stale}
						<span
							class="absolute inline-flex size-full animate-ping-slow rounded-full bg-green-fg opacity-60"
						></span>
					{/if}
					<span
						class={['relative inline-flex size-2 rounded-full', stale ? 'bg-amber' : 'bg-green-fg']}
					></span>
				</span>
				{stale ? `Last report ${ago(sampledAt, now)}` : `Live · ${ago(sampledAt, now)}`}
			</span>
		{/if}
	</div>

	{#if !data && !failed}
		<Skeleton class="h-72" />
	{:else if !sample}
		<p
			class="rounded-lg border border-line bg-canvas px-4 py-8 text-center text-[13px] text-fg-subtle"
		>
			{#if failed}
				Couldn't load what's running: {failed}
			{:else if at !== null}
				Nothing was recorded around {moment(at, now)}.
			{:else if device.measuredBy === 'hub'}
				Taking the first sample…
			{:else}
				Waiting for the agent to report what's running.
			{/if}
		</p>
	{:else}
		<p class="mb-2 text-[12.5px] text-fg-subtle">
			{#if total && metric === 'cpu'}
				Processes are using <span class="num text-fg">{cpuText(total.cpu)}</span> of the CPU{sample.cores
					? ` (${plural(sample.cores, 'core')})`
					: ''} across {plural(total.procs, 'process', 'processes')}
			{:else if total}
				<span class="num text-fg">{bytes(total.mem)}</span> resident across {plural(
					total.procs,
					'process',
					'processes'
				)}{device.metrics?.memory ? ` · ${bytes(device.metrics.memory.total)} installed` : ''}
			{:else}
				{view === 'groups' ? plural(count, 'group') : plural(count, 'process', 'processes')}
			{/if}
			{#if at !== null}<span class="text-fg-faint"> · one-minute average</span>{/if}
		</p>
		<div class="overflow-hidden rounded-lg border border-line">
			<div class={[grid, 'border-b border-line px-3 py-2 text-[12px] text-fg-subtle']}>
				<span>{view === 'groups' ? 'Service' : 'Process'}</span>
				{@render sortBy('cpu', 'CPU')}
				{@render sortBy('mem', 'Memory')}
			</div>
			{#if view === 'groups'}
				{#each groups.slice(0, shown) as g (g.key)}
					{@render group(g)}
				{/each}
			{:else}
				{#each procs.slice(0, shown) as p (`${p.pid}:${p.name}`)}
					{@render proc(p)}
				{/each}
			{/if}
			{#if count === 0}
				<p class="px-3 py-6 text-center text-[13px] text-fg-subtle">No processes in this sample.</p>
			{/if}
		</div>
		<div class="mt-2 flex flex-wrap items-center justify-between gap-2 text-[12px] text-fg-faint">
			<span>
				{#if view === 'groups'}
					Grouped into systemd units, containers, apps and login sessions.
				{:else if !isAdmin(page.data.me)}
					Only admins see command lines, which can hold passwords.
				{:else}
					The biggest processes by CPU and by memory. Click one for its whole command line.
				{/if}
			</span>
			{#if count > collapsed}
				<Button size="xs" variant="ghost" onclick={() => (showAll = !showAll)}>
					{showAll ? 'Show fewer' : `Show all ${count}`}
				</Button>
			{/if}
		</div>
	{/if}

	<UsageTrends {history} {metric} bind:range onSelect={pick} />
</Dialog>
