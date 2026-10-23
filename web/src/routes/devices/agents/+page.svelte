<script lang="ts">
	import Apple from '@lucide/svelte/icons/apple';
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import KeyRound from '@lucide/svelte/icons/key-round';
	import Monitor from '@lucide/svelte/icons/monitor';
	import Terminal from '@lucide/svelte/icons/terminal';
	import { resolve } from '$app/paths';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { forgetAgent } from '#lib/api/client.js';
	import type { Device } from '#lib/api/types.js';
	import { deviceIcon } from '#lib/device.js';
	import AgentInstall from '#lib/devices/AgentInstall.svelte';
	import { ago, osLabel, plural } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import type { Status } from '#lib/status.js';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	const devices = $derived(live.snapshot?.devices ?? []);
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());
	const reporting = $derived(devices.filter((d) => d.measuredBy).length);

	/** The devices whose agent Roost is forgetting. */
	let forgetting = $state<string[]>([]);

	interface AgentState {
		label: string;
		status: Status;
		/** A line about it, after the device's OS. */
		detail?: string;
	}

	function agentState(d: Device): AgentState {
		const a = d.agent;
		if (d.measuredBy === 'hub') {
			return { label: 'Built in', status: 'online', detail: 'Roost runs here and measures it' };
		}
		if (a) {
			const last = `last heartbeat ${ago(a.lastSeen, now)}`;
			if (!d.online) {
				return { label: 'Offline', status: 'unknown', detail: last };
			}
			if (a.stale) {
				return { label: 'Stopped reporting', status: 'attention', detail: last };
			}
			if (a.outdated) {
				return { label: 'Update available', status: 'attention', detail: `v${a.version}` };
			}
			return { label: 'Reporting', status: 'online', detail: `v${a.version}` };
		}
		if (['linux', 'macos'].includes(d.os.toLowerCase())) {
			return { label: 'Not installed', status: 'unknown' };
		}
		return { label: 'Not supported', status: 'unknown' };
	}

	const tone: Record<Status, string> = {
		online: 'text-fg-muted',
		attention: 'text-amber-fg',
		offline: 'text-red-fg',
		unknown: 'text-fg-faint'
	};

	async function forget(d: Device) {
		forgetting = [...forgetting, d.id];
		try {
			await forgetAgent(d.id);
		} catch {
			// forgetAgent showed the error as a toast.
		} finally {
			forgetting = forgetting.filter((id) => id !== d.id);
		}
	}
</script>

<a
	href={resolve('/devices')}
	class="mb-4 inline-flex items-center gap-1.5 text-[13px] text-fg-subtle transition-colors hover:text-fg"
>
	<ArrowLeft class="size-3.5" /> Devices
</a>
<PageHeader
	title="Agents"
	description={live.snapshot
		? `A small shell script that sends a machine's CPU, memory, disk, network, battery and processes to the hub every 15 seconds. ${reporting} of ${plural(devices.length, 'machine')} report.`
		: 'Connecting to the hub…'}
/>

<div class="grid grid-cols-[minmax(0,1fr)] gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
	<Card
		title="Install"
		description="Run it on any macOS or Linux machine on the tailnet. It installs for the user who runs it, without root."
	>
		<div class="space-y-4 p-4">
			<AdminOnly>
				<div>
					<div class="mb-2 flex items-center gap-2 text-[13px] font-medium text-fg-muted">
						<Apple class="size-3.5" /> macOS <span class="text-fg-faint">·</span>
						<Terminal class="size-3.5" /> Linux
					</div>
					<AgentInstall detailed />
				</div>
				<p
					class="flex items-start gap-2.5 rounded-lg border border-line bg-canvas px-3 py-2.5 text-[13px] text-fg-muted"
				>
					<KeyRound class="mt-0.5 size-4 shrink-0 text-fg-faint" />
					<span>
						Every agent sends the same token, which is in the install command, and Roost counts each
						heartbeat for the machine that Tailscale says sent it. Enrolling each machine on its own
						comes next, in
						<a
							href="https://github.com/manasseh-fikadu/roost/issues/8"
							target="_blank"
							rel="noreferrer"
							class="underline decoration-line-heavy underline-offset-4 hover:text-fg">#8</a
						>.
					</span>
				</p>
				{#snippet fallback()}
					<p class="text-[13px] text-fg-muted">
						The install command holds the token agents send, so only Roost's admins can see it. Ask
						one to install the agent.
					</p>
				{/snippet}
			</AdminOnly>
		</div>
	</Card>

	<Card title="Machines">
		{#if !live.snapshot}
			{#each [1, 2, 3] as i (i)}
				<div class="flex items-center gap-3 border-b border-line px-4 py-3 last:border-0">
					<Skeleton class="size-8" />
					<div class="flex-1 space-y-1.5">
						<Skeleton class="h-4 w-32" />
						<Skeleton class="h-3 w-48" />
					</div>
				</div>
			{/each}
		{:else if devices.length === 0}
			<EmptyState title="No machines yet" icon={Monitor}>Nothing is on the tailnet yet.</EmptyState>
		{:else}
			<ul class="divide-y divide-line">
				{#each devices as d (d.id)}
					{@const Icon = deviceIcon(d)}
					{@const info = agentState(d)}
					<li class="flex items-center gap-3 px-4 py-3">
						<span
							class="flex size-8 shrink-0 items-center justify-center rounded-md border border-line bg-raised text-fg-muted"
						>
							<Icon class="size-4" strokeWidth={1.6} />
						</span>
						<span class="min-w-0 flex-1">
							<a
								href={resolve('/devices/[id]', { id: d.id })}
								class="block truncate text-[14px] font-medium text-fg hover:underline"
							>
								{d.name}
							</a>
							<span class="block truncate font-mono text-[11px] text-fg-subtle">
								{osLabel(d.os)}{info.detail ? ` · ${info.detail}` : ''}
							</span>
						</span>
						<span class={['flex shrink-0 items-center gap-1.5 text-[12px]', tone[info.status]]}>
							<StatusDot status={info.status} />
							{info.label}
						</span>
						{#if d.agent}
							<AdminOnly>
								<Button
									size="xs"
									variant="ghost"
									loading={forgetting.includes(d.id)}
									title="Forget what this agent reported, such as after uninstalling it. An agent that still runs comes back with its next heartbeat."
									onclick={() => forget(d)}
								>
									Forget
								</Button>
							</AdminOnly>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</Card>
</div>
