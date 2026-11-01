<!--
	The last step of the setup, which is all optional: where alerts go, the
	agent on other machines, and SSH access to them. Each has one button that
	starts it.
-->
<script lang="ts">
	import type { Component } from 'svelte';
	import BellRing from '@lucide/svelte/icons/bell-ring';
	import Cpu from '@lucide/svelte/icons/cpu';
	import SquareTerminal from '@lucide/svelte/icons/square-terminal';
	import { resolve } from '$app/paths';
	import { agentRuns } from '#lib/agent.js';
	import type { Snapshot } from '#lib/api/types.js';
	import AddAgent from '#lib/devices/AddAgent.svelte';
	import { plural } from '#lib/format.js';
	import { sshCandidate } from '#lib/setup.js';
	import Card from '#lib/ui/Card.svelte';
	import { buttonClass } from '#lib/ui/button.js';

	interface Props {
		snapshot: Snapshot | null;
	}

	let { snapshot }: Props = $props();

	const devices = $derived(snapshot?.devices ?? []);
	/** The machines the agent runs on, and the ones that report. */
	const agentable = $derived(devices.filter((d) => agentRuns(d.os)));
	const measured = $derived(agentable.filter((d) => d.measuredBy));
	/** The machines Bield can log in to, those it does, and the first that it doesn't. */
	const loginable = $derived(devices.filter((d) => d.ssh));
	const loggedIn = $derived(loginable.filter((d) => d.ssh?.state !== 'not-set-up'));
	const first = $derived(sshCandidate(devices));
</script>

{#snippet row(Icon: Component, title: string, text: string)}
	<div class="flex min-w-0 flex-1 items-start gap-3">
		<span
			class="flex size-8 shrink-0 items-center justify-center rounded-md border border-line bg-raised text-fg-muted"
		>
			<Icon class="size-4" strokeWidth={1.6} />
		</span>
		<div class="min-w-0">
			<p class="text-[14px] font-medium text-fg">{title}</p>
			<p class="mt-0.5 text-[13px] text-fg-subtle">{text}</p>
		</div>
	</div>
{/snippet}

<Card
	title="Connect more"
	description="All of this is optional, and each part is here in the app when you want it."
>
	<ul class="divide-y divide-line">
		<li class="flex flex-wrap items-center gap-3 px-4 py-3">
			{@render row(
				BellRing,
				'Get told when something breaks',
				'Send alerts to ntfy, Discord, Slack, Telegram, email or a webhook, so you hear about a problem without opening Bield.'
			)}
			<a href="{resolve('/settings')}?tab=notifications" class={buttonClass('secondary', 'sm')}>
				Add a channel
			</a>
		</li>
		<li class="flex flex-wrap items-center gap-3 px-4 py-3">
			{@render row(
				Cpu,
				'Install the agent for CPU, memory and disk',
				agentable.length === 0
					? 'Bield measures the machine it runs on. The agent does the same on a Mac or a Linux machine.'
					: `${measured.length} of ${plural(agentable.length, 'machine')} report their CPU, memory and disk.`
			)}
			<AddAgent label="Add agent" />
		</li>
		<li class="flex flex-wrap items-center gap-3 px-4 py-3">
			{@render row(
				SquareTerminal,
				'Set up access for terminal, files and Docker',
				loginable.length === 0
					? 'Bield can log in to Linux, macOS and FreeBSD machines over SSH.'
					: `Bield can log in to ${loggedIn.length} of ${plural(loginable.length, 'machine')}.`
			)}
			{#if first}
				<a
					href="{resolve('/devices/[id]', { id: first.id })}#ssh"
					class={buttonClass('secondary', 'sm')}
				>
					Set up {first.name}
				</a>
			{/if}
		</li>
	</ul>
</Card>
