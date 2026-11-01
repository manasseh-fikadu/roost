<script lang="ts">
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import BatteryCharging from '@lucide/svelte/icons/battery-charging';
	import BatteryMedium from '@lucide/svelte/icons/battery-medium';
	import Bot from '@lucide/svelte/icons/bot';
	import Monitor from '@lucide/svelte/icons/monitor';
	import Pencil from '@lucide/svelte/icons/pencil';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { agentRuns } from '#lib/agent.js';
	import { onDevice } from '#lib/alerts.js';
	import AlertsCard from '#lib/alerts/AlertsCard.svelte';
	import type { Device } from '#lib/api/types.js';
	import { deviceIcon } from '#lib/device.js';
	import AddAgent from '#lib/devices/AddAgent.svelte';
	import DeviceActions from '#lib/devices/DeviceActions.svelte';
	import DeviceHistory from '#lib/devices/DeviceHistory.svelte';
	import DeviceSettingsDialog from '#lib/devices/DeviceSettingsDialog.svelte';
	import MetricTile from '#lib/devices/MetricTile.svelte';
	import ServicesCard from '#lib/devices/ServicesCard.svelte';
	import SSHCard from '#lib/devices/SSHCard.svelte';
	import UsagePanel from '#lib/devices/UsagePanel.svelte';
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
	import { historyRanges, type HistoryRange } from '#lib/history.js';
	import { live } from '#lib/live.svelte.js';
	import { linkLabel, tailnetIP } from '#lib/tailnet.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import DataRow from '#lib/ui/DataRow.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusPill from '#lib/ui/StatusPill.svelte';
	import type { UsageMetric } from '#lib/usage.js';

	const device = $derived(live.snapshot?.devices.find((d) => d.id === page.params.id));
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());
	const devices = $derived(live.snapshot?.devices ?? []);
	// On the machine Roost runs on, which can be two nodes, either's alerts
	// and services count.
	const alerts = $derived(device ? onDevice(live.snapshot?.alerts ?? [], device, devices) : []);
	const units = $derived(device ? onDevice(live.snapshot?.units ?? [], device, devices) : []);
	const containers = $derived(
		device ? onDevice(live.snapshot?.containers ?? [], device, devices) : []
	);

	let editing = $state(false);
	let range: HistoryRange = $state('24h');
	let usageOpen = $state(false);
	let usageMetric: UsageMetric = $state('cpu');
	let usageAt = $state<number | null>(null);

	/** Opens what's using the device's CPU or memory, now or at a time. */
	function showUsage(metric: UsageMetric, at: number | null = null) {
		usageMetric = metric;
		usageAt = at;
		usageOpen = true;
	}

	/** Whether the device's metrics are out of date, from an agent that stopped or a device that's off. */
	function stale(d: Device): boolean {
		return d.measuredBy === 'agent' && (!d.online || !!d.agent?.stale);
	}

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

{#snippet agentsLink(text = 'Agents')}
	<a
		href={resolve('/devices/agents')}
		class="text-fg-muted underline decoration-line-heavy underline-offset-4 hover:text-fg">{text}</a
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

	<DeviceActions {device} />

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
		{#if stale(device) && device.agent}
			<span class="font-mono text-[12px] text-fg-subtle"
				>Metrics from {ago(device.agent.lastSeen, now)}</span
			>
		{/if}
	</p>

	{#if m}
		<div
			class={[
				'mb-4 grid grid-cols-2 gap-3 transition-opacity md:grid-cols-3 xl:grid-cols-6',
				stale(device) && 'opacity-60'
			]}
		>
			<MetricTile
				label="CPU"
				value={percent(m.cpu, 1)}
				meter={m.cpu ?? null}
				warn={75}
				crit={90}
				sub={m.load ? `load ${m.load.map((l) => l.toFixed(2)).join(' ')}` : undefined}
				onclick={() => showUsage('cpu')}
			/>
			<MetricTile
				label="Memory"
				value={percent(ratio(m.memory?.used, m.memory?.total))}
				meter={ratio(m.memory?.used, m.memory?.total)}
				warn={80}
				crit={92}
				sub="{bytes(m.memory?.used)} of {bytes(m.memory?.total)}"
				onclick={() => showUsage('mem')}
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
			{#if m.battery}
				{@const b = m.battery}
				<MetricTile
					label="Battery"
					value={percent(b.percent)}
					sub={b.charging ? 'Charging' : b.onAc ? 'On power' : 'On battery'}
				>
					{#snippet icon()}
						{#if b.charging}
							<BatteryCharging class="size-5 text-green-fg" />
						{:else}
							<BatteryMedium class="size-5 text-fg-muted" />
						{/if}
					{/snippet}
				</MetricTile>
			{:else if m.temp !== undefined}
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
			{#if alerts.length > 0}
				<AlertsCard {alerts} {devices} {now} />
			{/if}
			{#if device.agent && !device.measuredBy}
				<Card
					title="Waiting for the agent"
					description="The agent enrolled {ago(
						device.agent.enrolledAt,
						now
					)}, and its first heartbeat hasn't come yet."
				>
					<p class="p-4 text-[13px] text-fg-muted">
						On {device.name}, its log says why: on macOS,
						<code class="font-mono text-[12px]">~/Library/Logs/roost-agent.log</code>, and on Linux,
						<code class="font-mono text-[12px]">journalctl --user -u roost-agent</code>.
					</p>
				</Card>
			{:else if !device.measuredBy}
				<Card
					title={agentRuns(device.os)
						? 'Install the agent for CPU, memory and disk'
						: 'No agent on this machine'}
					description="Tailscale shows whether it's connected and how the hub reaches it. The agent adds CPU, memory, disk, battery and what's running."
				>
					<div class="space-y-3 p-4 text-[13px] text-fg-muted">
						{#if agentRuns(device.os)}
							<AdminOnly>
								<p>
									Run one command on {device.name}, or let Roost run it over SSH once it can log in.
									It installs a small background service for your user, without root.
								</p>
								<AddAgent {device} label="Install the agent" variant="primary" />
								{#snippet fallback()}
									<p>An admin can install the agent from the {@render agentsLink()} page.</p>
								{/snippet}
							</AdminOnly>
						{:else}
							<p>
								The agent runs on macOS and Linux. On {osLabel(device.os)}, Roost shows what
								Tailscale knows.
							</p>
						{/if}
					</div>
				</Card>
			{/if}
			<Card title="History">
				{#snippet actions()}
					<Segmented bind:value={range} options={historyRanges} label="Time range" />
				{/snippet}
				<div class="p-4">
					<DeviceHistory
						{device}
						{range}
						onSelect={device.measuredBy ? (ts) => showUsage('cpu', ts) : undefined}
					/>
				</div>
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
				{#snippet actions()}
					{#if device.agent?.version}
						<span class="flex items-center gap-1 font-mono text-[11px] text-fg-subtle">
							<Bot class="size-3" /> agent v{device.agent.version}
						</span>
					{:else if device.measuredBy === 'hub'}
						<span class="font-mono text-[11px] text-fg-subtle">measured by the hub</span>
					{/if}
				{/snippet}
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
						{#if device.agent && (device.agent.outdated || !device.agent.enrolledAt)}
							{@const enrolled = !!device.agent.enrolledAt}
							<DataRow label="Agent">
								<span class="flex items-center justify-end gap-2">
									<span class="text-amber-fg">{enrolled ? 'Update available' : 'Not enrolled'}</span
									>
									<AdminOnly>
										<AddAgent
											{device}
											label={enrolled ? 'Update' : 'Re-enroll'}
											size="xs"
											variant="ghost"
										/>
									</AdminOnly>
								</span>
							</DataRow>
						{/if}
					</dl>
				{:else}
					<p class="px-4 py-4 text-[13px] text-fg-subtle">No system details without the agent.</p>
				{/if}
			</Card>
			{#if device.ssh}
				<!-- Only admins get device.ssh. -->
				<SSHCard {device} {now} />
			{/if}
			{#if device.measuredBy === 'hub' && (live.snapshot?.collectors ?? []).some((c) => c.name === 'systemd units')}
				<ServicesCard {units} {containers} {now} />
			{/if}
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
	{#if device.measuredBy}
		<UsagePanel
			{device}
			bind:open={usageOpen}
			bind:metric={usageMetric}
			bind:at={usageAt}
			bind:range
		/>
	{/if}
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
