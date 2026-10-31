<!--
	The container page's first tab: what the container uses now (from the latest
	live sample), what went wrong with it, its health check, and how it is set
	up. Commands are for admins, and so is the link to the compose directory in
	Files.
-->
<script lang="ts">
	import FolderOpen from '@lucide/svelte/icons/folder-open';
	import { resolve } from '$app/paths';
	import type { DockerContainerDetail, DockerStats } from '#lib/api/types.js';
	import MetricTile from '#lib/devices/MetricTile.svelte';
	import { ago, bytes, dateTime, duration, percent, plural, rate, ratio } from '#lib/format.js';
	import Card from '#lib/ui/Card.svelte';
	import CopyButton from '#lib/ui/CopyButton.svelte';
	import DataRow from '#lib/ui/DataRow.svelte';
	import { argv, shortId } from '../docker.js';
	import HealthCard from './HealthCard.svelte';
	import {
		composeFileNames,
		crash,
		restartPolicyLabel,
		secondsSince,
		shortWhen
	} from './helpers.js';
	import PortsCard from './PortsCard.svelte';

	interface Props {
		container: DockerContainerDetail;
		/** The live samples, oldest first. The tiles show the last. */
		samples: DockerStats[];
		/** The time to measure ages from, in Unix milliseconds. */
		now: number;
		admin: boolean;
		deviceId: string;
		/** Whether the compose directory can open in Files. */
		canBrowse: boolean;
	}

	let { container: d, samples, now, admin, deviceId, canBrowse }: Props = $props();

	const running = $derived(d.state.running);
	const s = $derived(running ? samples.at(-1) : undefined);
	const cpuCap = $derived(d.resources.cpus ?? s?.cpus ?? null);
	const memLimit = $derived(d.resources.memory ?? s?.memLimit ?? null);
	const up = $derived(secondsSince(d.state.startedAt, now));
	const down = $derived(secondsSince(d.state.finishedAt, now));
	const problem = $derived(crash(d.state));
	const caps = $derived(
		[...d.security.capAdd.map((c) => `+${c}`), ...d.security.capDrop.map((c) => `−${c}`)].join(' ')
	);

	/** A time with how long ago it was, such as "Oct 17, 9:36 PM · 2h ago". */
	function when(t: string | undefined): string {
		return t ? `${dateTime(t)} · ${ago(t, now)}` : '—';
	}
</script>

{#snippet commandRow(label: string, args: string)}
	<div class="border-b border-line px-4 py-2 last:border-0">
		<div class="flex items-center justify-between gap-2">
			<dt class="text-[13px] text-fg-subtle">{label}</dt>
			{#if args}
				<dd class="-mr-2"><CopyButton value={args} label="Copy {label.toLowerCase()}" /></dd>
			{/if}
		</div>
		<dd class="pb-1 font-mono text-[12.5px] break-all text-fg">{args || '—'}</dd>
	</div>
{/snippet}

<div class="mb-4 grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-7">
	<MetricTile
		label="CPU"
		value={s ? percent(s.cpu, 1) : '—'}
		meter={s ? (s.cpu != null && cpuCap ? s.cpu / cpuCap : null) : undefined}
		warn={75}
		crit={90}
		sub={d.resources.cpus
			? `limit ${d.resources.cpus} CPU`
			: s
				? `of ${plural(s.cpus, 'core')}`
				: undefined}
	/>
	<MetricTile
		label="Memory"
		value={s ? bytes(s.memUsed) : '—'}
		meter={s ? ratio(s.memUsed, memLimit) : undefined}
		sub={memLimit ? `of ${bytes(memLimit, 0)}${d.resources.memory ? '' : ' (host)'}` : undefined}
	/>
	<MetricTile
		label="Network"
		small
		value={s ? `↓ ${rate(s.netRx)}` : '—'}
		sub={s ? `↑ ${rate(s.netTx)}` : undefined}
	/>
	<MetricTile
		label="Disk I/O"
		small
		value={s ? `R ${rate(s.blkReadRate)}` : '—'}
		sub={s ? `W ${rate(s.blkWriteRate)} · ${bytes(s.blkWrite)} written` : undefined}
	/>
	<MetricTile
		label={running ? 'Uptime' : 'Stopped'}
		value={running ? duration(up) : down != null ? `${duration(down)} ago` : '—'}
		sub={running
			? d.state.paused
				? 'paused'
				: `since ${shortWhen(d.state.startedAt, now)}`
			: d.state.exitCode != null
				? `exit code ${d.state.exitCode}`
				: undefined}
	/>
	<MetricTile
		label="Restarts"
		value={String(d.restartCount)}
		sub={d.restartPolicy ? `policy: ${d.restartPolicy.name}` : 'no restart policy'}
	/>
	<MetricTile
		label="PIDs"
		value={s?.pids != null ? String(s.pids) : '—'}
		sub={d.resources.pidsLimit ? `limit ${d.resources.pidsLimit}` : 'no limit'}
	/>
</div>

{#if problem}
	<div
		role="alert"
		class="mb-4 rounded-lg border border-red/30 bg-red/5 px-4 py-2.5 text-[13px] text-red-fg"
	>
		{problem.title}
		{#if problem.at}
			<span class="font-mono text-[12px] text-fg-subtle"> · {ago(problem.at, now)}</span>
		{/if}
		{#if problem.error}
			<div class="mt-1 font-mono text-[12px] break-words">{problem.error}</div>
		{/if}
	</div>
{/if}

<div class="grid grid-cols-[minmax(0,1fr)] gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
	<div class="min-w-0 space-y-4">
		<HealthCard container={d} {now} />
		<Card title="Configuration">
			<dl>
				<DataRow label="Image" copy={d.image}>{d.image}</DataRow>
				<DataRow label="Image ID" copy={d.imageId}>
					{shortId(d.imageId)}{#if d.imageSize != null}<span class="text-fg-subtle">
							· {bytes(d.imageSize)}</span
						>{/if}
				</DataRow>
				{#if d.imageDigest}
					<DataRow label="Digest" copy={d.imageDigest}>
						{d.imageDigest.split('@')[1] ?? d.imageDigest}
					</DataRow>
				{/if}
				{#if admin}
					{#if d.entrypoint?.length}
						{@render commandRow('Entrypoint', argv(d.entrypoint))}
					{/if}
					{@render commandRow('Command', argv(d.cmd))}
				{:else}
					<DataRow label="Command">
						<span class="text-fg-subtle">Only admins can see commands</span>
					</DataRow>
				{/if}
				<DataRow label="Working dir">{d.workingDir || '/'}</DataRow>
				<DataRow label="User">{d.user || 'image default'}</DataRow>
				<DataRow label="Hostname">{d.hostname || '—'}</DataRow>
				<DataRow label="TTY">{d.tty ? 'yes' : 'no'}</DataRow>
				<DataRow label="Log driver">{d.logDriver || '—'}</DataRow>
				{#if d.platform}
					<DataRow label="Platform">{d.platform}</DataRow>
				{/if}
			</dl>
		</Card>
	</div>
	<div class="min-w-0 space-y-4">
		<Card title="State">
			<dl>
				<DataRow label="Status">{d.state.status}</DataRow>
				<DataRow label="Created">{when(d.created)}</DataRow>
				<DataRow label="Started">{when(d.state.startedAt)}</DataRow>
				{#if !running}
					<DataRow label="Finished">{when(d.state.finishedAt)}</DataRow>
					<DataRow label="Exit code">{d.state.exitCode ?? '—'}</DataRow>
				{/if}
				<DataRow label="OOM killed">{d.state.oomKilled ? 'yes' : 'no'}</DataRow>
				{#if d.state.pid}
					<DataRow label="Host PID">{d.state.pid}</DataRow>
				{/if}
				<DataRow label="Restart policy">{restartPolicyLabel(d.restartPolicy, d.autoRemove)}</DataRow
				>
			</dl>
		</Card>
		<PortsCard ports={d.ports} />
		<Card title="Resources">
			<dl>
				<DataRow label="Memory limit"
					>{d.resources.memory ? bytes(d.resources.memory) : 'none'}</DataRow
				>
				{#if d.resources.memoryReservation}
					<DataRow label="Reservation">{bytes(d.resources.memoryReservation)}</DataRow>
				{/if}
				{#if d.resources.memorySwap}
					<DataRow label="Memory + swap">
						{d.resources.memorySwap < 0 ? 'unlimited' : bytes(d.resources.memorySwap)}
					</DataRow>
				{/if}
				<DataRow label="CPU limit">{d.resources.cpus ? `${d.resources.cpus} CPU` : 'none'}</DataRow>
				{#if d.resources.cpuShares}
					<DataRow label="CPU shares">{d.resources.cpuShares}</DataRow>
				{/if}
				{#if d.resources.cpuset}
					<DataRow label="CPU set">{d.resources.cpuset}</DataRow>
				{/if}
				<DataRow label="PIDs limit">{d.resources.pidsLimit ?? 'none'}</DataRow>
				{#if d.resources.shmSize}
					<DataRow label="/dev/shm">{bytes(d.resources.shmSize)}</DataRow>
				{/if}
				<DataRow label="Privileged">
					{#if d.security.privileged}<span class="text-amber-fg">yes</span>{:else}no{/if}
				</DataRow>
				<DataRow label="Read-only root">{d.security.readonlyRootfs ? 'yes' : 'no'}</DataRow>
				{#if caps}
					<DataRow label="Capabilities">
						<span class="block font-mono text-[12px] whitespace-normal">{caps}</span>
					</DataRow>
				{/if}
				{#if d.security.securityOpt.length > 0}
					<DataRow label="Security opts">
						<span class="block whitespace-normal">{d.security.securityOpt.join(', ')}</span>
					</DataRow>
				{/if}
			</dl>
		</Card>
		{#if d.project}
			<Card title="Compose">
				<dl>
					<DataRow label="Project">{d.project}</DataRow>
					{#if d.service}
						<DataRow label="Service">{d.service}</DataRow>
					{/if}
					{#if d.composeDir}
						<DataRow label="Directory" copy={d.composeDir}>
							{#if canBrowse}
								<a
									href="{resolve('/files')}?device={encodeURIComponent(
										deviceId
									)}&path={encodeURIComponent(d.composeDir)}"
									title="Open in Files"
									class="inline-flex items-center gap-1 hover:underline"
								>
									<FolderOpen class="size-3.5 shrink-0 text-fg-subtle" />
									{d.composeDir}
								</a>
							{:else}
								{d.composeDir}
							{/if}
						</DataRow>
					{/if}
					{#if d.composeFiles}
						<DataRow label="Files">{composeFileNames(d.composeFiles)}</DataRow>
					{/if}
				</dl>
			</Card>
		{/if}
	</div>
</div>
