<script lang="ts">
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import LineChart from '@lucide/svelte/icons/chart-line';
	import Monitor from '@lucide/svelte/icons/monitor';
	import Pencil from '@lucide/svelte/icons/pencil';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import type { Device } from '#lib/api/types.js';
	import { deviceIcon } from '#lib/device.js';
	import DeviceSettingsDialog from '#lib/devices/DeviceSettingsDialog.svelte';
	import MetricTile from '#lib/devices/MetricTile.svelte';
	import RttHistory from '#lib/devices/RttHistory.svelte';
	import {
		ago,
		bytes,
		dateTime,
		duration,
		latency,
		osLabel,
		percent,
		rate,
		ratio,
		shortDate
	} from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import { linkLabel, tailnetIP } from '#lib/tailnet.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import DataRow from '#lib/ui/DataRow.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusPill from '#lib/ui/StatusPill.svelte';

	const device = $derived(live.snapshot?.devices.find((d) => d.id === page.params.id));
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());

	let editing = $state(false);

	const bannerClass = {
		online: 'border-line bg-surface text-fg-muted',
		attention: 'border-amber/25 bg-amber/5 text-amber-fg',
		offline: 'border-red/30 bg-red/5 text-red-fg',
		unknown: 'border-line bg-surface text-fg-muted'
	};

	function ipv6(d: Device): string | undefined {
		return d.ips.find((ip) => ip.includes(':'));
	}

	/** When a key expires, and how long until then or since. */
	function expiry(t: string): string {
		const when = ago(t, now);
		return `${shortDate(t)} (${when.startsWith('in ') ? when.slice(3) : when})`;
	}
</script>

{#snippet issue(n: number)}
	<a
		href="https://github.com/manasseh-fikadu/roost/issues/{n}"
		target="_blank"
		rel="noreferrer"
		class="text-fg-muted underline decoration-line-heavy underline-offset-4 hover:text-fg">#{n}</a
	>
{/snippet}

{#if device}
	{@const Icon = deviceIcon(device)}
	{@const m = device.metrics}
	{@const info = device.info}
	<a
		href={resolve('/devices')}
		class="mb-4 inline-flex items-center gap-1.5 text-[13px] text-fg-subtle transition-colors hover:text-fg"
	>
		<ArrowLeft class="size-3.5" /> Devices
	</a>
	<div class="mb-6 flex flex-wrap items-start justify-between gap-4">
		<div class="flex min-w-0 items-center gap-4">
			<div
				class="flex size-12 shrink-0 items-center justify-center rounded-xl border border-line-strong bg-raised text-fg"
			>
				<Icon class="size-6" strokeWidth={1.6} />
			</div>
			<div class="min-w-0">
				<div class="flex flex-wrap items-center gap-2">
					<h1 class="truncate text-[24px] leading-8 font-semibold tracking-[-0.02em] text-fg">
						{device.name}
					</h1>
					<StatusPill status={device.status} />
					{#if device.self}
						<Badge tone="blue" mono>HUB</Badge>
					{/if}
				</div>
				<p class="mt-0.5 truncate font-mono text-[12px] text-fg-subtle">
					{device.dnsName} · {info?.osVersion || osLabel(device.os)}
				</p>
			</div>
		</div>
		<AdminOnly>
			<Button onclick={() => (editing = true)}>
				{#snippet icon()}<Pencil class="size-3.5" />{/snippet}
				Edit
			</Button>
		</AdminOnly>
	</div>

	<p
		class={[
			'mb-4 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border px-4 py-2.5 text-[13px]',
			bannerClass[device.status]
		]}
	>
		<span>{device.statusReason}</span>
		{#if !device.online && device.lastSeen}
			<span class="font-mono text-[12px] text-fg-subtle">Last seen {ago(device.lastSeen, now)}</span
			>
		{/if}
	</p>

	{#if m}
		<div class="mb-4 grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
			<MetricTile
				label="CPU"
				value={percent(m.cpu, 1)}
				meter={m.cpu ?? null}
				warn={75}
				crit={90}
				sub={m.load ? `load ${m.load.map((l) => l.toFixed(2)).join(' ')}` : undefined}
			/>
			<MetricTile
				label="Memory"
				value={percent(ratio(m.memory?.used, m.memory?.total))}
				meter={ratio(m.memory?.used, m.memory?.total)}
				warn={80}
				crit={92}
				sub="{bytes(m.memory?.used)} of {bytes(m.memory?.total)}"
			/>
			<MetricTile
				label="Disk"
				value={percent(ratio(m.disk?.used, m.disk?.total))}
				meter={ratio(m.disk?.used, m.disk?.total)}
				warn={85}
				crit={95}
				sub={m.disk ? `${bytes(m.disk.total - m.disk.used)} free` : undefined}
			/>
			<MetricTile label="Network" value="↓ {rate(m.netRx)}" small sub="↑ {rate(m.netTx)}" />
			<MetricTile
				label="Uptime"
				value={duration(m.uptime)}
				sub={m.procs ? `${m.procs} processes` : undefined}
			/>
			{#if m.temp !== undefined}
				<MetricTile label="Temperature" value="{m.temp.toFixed(0)}°C" />
			{:else if m.swap}
				<MetricTile
					label="Swap"
					value={percent(ratio(m.swap.used, m.swap.total))}
					sub="{bytes(m.swap.used)} of {bytes(m.swap.total)}"
				/>
			{/if}
		</div>
	{/if}

	<div class="grid grid-cols-[minmax(0,1fr)] gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
		<div class="min-w-0 space-y-4">
			{#if !m}
				<Card
					title="No agent on this machine"
					description="Tailscale shows whether it's connected and how the hub reaches it."
				>
					<p class="px-4 py-3 text-[13px] text-fg-subtle">
						The agent adds CPU, memory, disk, battery and temperature, and it comes with
						{@render issue(24)}.
					</p>
				</Card>
			{/if}
			<Card title="History">
				{#if device.self}
					<EmptyState title="Coming soon" icon={LineChart}>
						CPU, memory and network history come with {@render issue(24)}.
					</EmptyState>
				{:else}
					<div class="p-4">
						<RttHistory deviceId={device.id} />
					</div>
				{/if}
			</Card>
		</div>

		<div class="space-y-4">
			<Card title="Tailscale">
				<dl>
					<DataRow label="Tailnet IP" copy={tailnetIP(device)}>{tailnetIP(device) ?? '—'}</DataRow>
					{#if ipv6(device) && ipv6(device) !== tailnetIP(device)}
						<DataRow label="IPv6" copy={ipv6(device)}>{ipv6(device)}</DataRow>
					{/if}
					<DataRow label="MagicDNS" copy={device.dnsName}>{device.dnsName}</DataRow>
					<DataRow label="Path">{linkLabel(device)}</DataRow>
					{#if device.path.endpoint}
						<DataRow label="Endpoint">{device.path.endpoint}</DataRow>
					{/if}
					{#if device.path.peerRelay}
						<DataRow label="Peer relay">{device.path.peerRelay}</DataRow>
					{/if}
					<DataRow label="Home DERP">{device.path.homeDerp ?? '—'}</DataRow>
					{#if !device.self}
						<DataRow label="RTT">
							{device.path.unanswered ? 'no reply' : latency(device.path.rttMs)}
						</DataRow>
						<DataRow label="Pinged">
							{device.path.pingedAt ? ago(device.path.pingedAt, now) : 'not yet'}
						</DataRow>
						<DataRow label="Traffic">↓{bytes(device.rxBytes)} ↑{bytes(device.txBytes)}</DataRow>
					{/if}
					{#if device.exitNode}
						<DataRow label="Exit node">Roost's traffic goes through it</DataRow>
					{/if}
					<DataRow label="Key expiry">
						{device.keyExpiry ? expiry(device.keyExpiry) : 'disabled'}
					</DataRow>
					<DataRow label="Added">{shortDate(device.created)}</DataRow>
				</dl>
			</Card>
			<Card title="System">
				{#if info}
					<dl>
						<DataRow label="Hostname">{info.hostname || device.hostName}</DataRow>
						<DataRow label="OS">{info.osVersion || osLabel(device.os)}</DataRow>
						{#if info.kernel}
							<!-- Only admins get the kernel. -->
							<DataRow label="Kernel">{info.kernel}</DataRow>
						{/if}
						<DataRow label="Model">{info.model || '—'}</DataRow>
						<DataRow label="Arch">{info.arch || '—'}</DataRow>
						<DataRow label="Cores">{info.cores || '—'}</DataRow>
						{#if m}
							<DataRow label="Last report">{ago(m.at, now)} · {dateTime(m.at)}</DataRow>
						{/if}
					</dl>
				{:else}
					<p class="px-4 py-4 text-[13px] text-fg-subtle">No system details without the agent.</p>
				{/if}
			</Card>
			{#if device.settings.notes}
				<Card title="Notes">
					<p class="px-4 py-3 text-[13px] whitespace-pre-wrap text-fg-muted">
						{device.settings.notes}
					</p>
				</Card>
			{/if}
		</div>
	</div>
	<AdminOnly>
		<DeviceSettingsDialog {device} bind:open={editing} />
	</AdminOnly>
{:else if live.snapshot}
	<EmptyState title="No device here" icon={Monitor}>
		It may have left the tailnet. <a
			href={resolve('/devices')}
			class="text-fg underline underline-offset-4">See every device</a
		>.
	</EmptyState>
{:else}
	<Skeleton class="h-4 w-20" />
	<div class="mt-6 flex items-center gap-4">
		<Skeleton class="size-12 rounded-xl" />
		<div class="space-y-2">
			<Skeleton class="h-7 w-48" />
			<Skeleton class="h-4 w-72" />
		</div>
	</div>
	<div class="mt-6 grid gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
		<Skeleton class="h-64 rounded-xl" />
		<Skeleton class="h-64 rounded-xl" />
	</div>
{/if}
