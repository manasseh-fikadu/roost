<script lang="ts">
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import KeyRound from '@lucide/svelte/icons/key-round';
	import Monitor from '@lucide/svelte/icons/monitor';
	import Plus from '@lucide/svelte/icons/plus';
	import { resolve } from '$app/paths';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { agentRuns, agentState } from '#lib/agent.js';
	import { revokeAgent } from '#lib/api/client.js';
	import type { Device } from '#lib/api/types.js';
	import { deviceIcon } from '#lib/device.js';
	import AddAgent from '#lib/devices/AddAgent.svelte';
	import AgentUninstall from '#lib/devices/AgentUninstall.svelte';
	import UninstallAgent from '#lib/devices/UninstallAgent.svelte';
	import { dateTime, osLabel, plural } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import type { Status } from '#lib/status.js';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	const devices = $derived(live.snapshot?.devices ?? []);
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());
	const reporting = $derived(devices.filter((d) => d.measuredBy).length);

	/** The device whose agent an admin is about to revoke. */
	let revoking = $state<{ id: string; name: string } | null>(null);
	let confirmOpen = $state(false);
	let busy = $state(false);

	const tone: Record<Status, string> = {
		online: 'text-fg-muted',
		attention: 'text-amber-fg',
		offline: 'text-red-fg',
		unknown: 'text-fg-faint'
	};

	function askRevoke(d: Device) {
		revoking = { id: d.id, name: d.name };
		confirmOpen = true;
	}

	async function revoke() {
		if (!revoking) {
			return;
		}
		busy = true;
		try {
			await revokeAgent(revoking.id);
			confirmOpen = false;
		} catch {
			// revokeAgent showed the error as a toast.
		} finally {
			busy = false;
		}
	}

	/** When a device's agent enrolled, and who let it, for a tooltip. */
	function enrolledTitle(d: Device): string | undefined {
		const a = d.agent;
		if (!a?.enrolledAt) {
			return undefined;
		}
		return `Enrolled ${dateTime(a.enrolledAt)}${a.enrolledBy ? ` with a code from ${a.enrolledBy}` : ''}`;
	}
</script>

{#snippet plus()}
	<Plus class="size-3.5" />
{/snippet}

<a
	href={resolve('/devices')}
	class="mb-4 inline-flex items-center gap-1.5 text-[13px] text-fg-subtle transition-colors hover:text-fg pointer-coarse:-mt-2 pointer-coarse:mb-2 pointer-coarse:py-2"
>
	<ArrowLeft class="size-3.5" /> Devices
</a>
<PageHeader
	title="Agents"
	description={live.snapshot
		? `A small shell script that sends a machine's CPU, memory, disk, network, battery and processes to the hub every 15 seconds. ${reporting} of ${plural(devices.length, 'machine')} report.`
		: 'Connecting to the hub…'}
>
	{#snippet actions()}
		<AdminOnly>
			<AddAgent variant="primary" icon={plus} />
		</AdminOnly>
	{/snippet}
</PageHeader>

<div class="grid grid-cols-[minmax(0,1fr)] gap-4 xl:grid-cols-[minmax(0,1fr)_400px]">
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
					{@const info = agentState(d, now)}
					<li class="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
						<span
							class="flex size-8 shrink-0 items-center justify-center rounded-md border border-line bg-raised text-fg-muted"
						>
							<Icon class="size-4" strokeWidth={1.6} />
						</span>
						<span class="min-w-0 flex-1">
							<a
								href={resolve('/devices/[id]', { id: d.id })}
								class="block truncate text-[14px] font-medium text-fg hover:underline pointer-coarse:-my-1.5 pointer-coarse:py-1.5"
							>
								{d.name}
							</a>
							<span
								class="block truncate font-mono text-[11px] text-fg-subtle"
								title={enrolledTitle(d)}
							>
								{osLabel(d.os)}{info.detail ? ` · ${info.detail}` : ''}
							</span>
						</span>
						<span class={['flex shrink-0 items-center gap-1.5 text-[12px]', tone[info.status]]}>
							<StatusDot status={info.status} />
							{info.label}
						</span>
						<AdminOnly>
							{#if d.agent}
								<span class="flex shrink-0 gap-1">
									<AddAgent
										device={d}
										label={d.agent.outdated ? 'Update' : 'Re-enroll'}
										size="xs"
										variant="ghost"
									/>
									{#if d.ssh?.state === 'ok'}
										<UninstallAgent device={d} />
									{/if}
									<Button size="xs" variant="ghost" onclick={() => askRevoke(d)}>Revoke</Button>
								</span>
							{:else if d.measuredBy !== 'hub' && agentRuns(d.os)}
								<AddAgent device={d} label="Add" size="xs" variant="ghost" />
							{/if}
						</AdminOnly>
					</li>
				{/each}
			</ul>
		{/if}
	</Card>

	<Card title="How it works">
		<div class="space-y-3 p-4 text-[13px] text-fg-muted">
			<p>
				Add agent makes an install command for macOS or Linux. It works on one machine for an hour,
				and installs the agent for the user who runs it, without root. On a machine Roost can log in
				to over SSH, it can run the install for you.
			</p>
			<p class="flex items-start gap-2.5">
				<KeyRound class="mt-0.5 size-4 shrink-0 text-fg-faint" />
				<span>
					The agent trades the command's code for a secret of its own, which only its user can read.
					Roost takes its heartbeats only from the machine it enrolled on, as Tailscale says, and
					stops taking them when an admin revokes it.
				</span>
			</p>
			<p>
				On macOS it runs as a LaunchAgent, which logs to
				<code class="font-mono text-[12px]">~/Library/Logs/roost-agent.log</code>. On Linux it's a
				systemd user service:
				<code class="font-mono text-[12px]">journalctl --user -u roost-agent</code>.
			</p>
			<AdminOnly>
				<AgentUninstall />
				{#snippet fallback()}
					<p>Only Roost's admins can add or remove agents.</p>
				{/snippet}
			</AdminOnly>
		</div>
	</Card>
</div>

<Dialog
	bind:open={confirmOpen}
	title="Revoke {revoking?.name ?? 'this'}'s agent?"
	description="Roost will refuse its heartbeats and forget what it reported. To add it back, run a new install command on the machine."
>
	<p class="text-[13px] text-fg-muted">
		The agent keeps running until it's uninstalled, and tries again every few minutes.
	</p>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (confirmOpen = false)}>Cancel</Button>
		<Button variant="danger" loading={busy} onclick={revoke}>Revoke</Button>
	{/snippet}
</Dialog>
