<!--
	A button that makes a new command to install the agent, and shows it in a
	dialog until a machine enrolls with it and reports. With `device`, the
	command is for that device: to add the agent, or to enroll it again,
	which also updates it. If Roost can log in to the device over SSH, the
	dialog offers to run the install there. Only admins can make one.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import CircleCheck from '@lucide/svelte/icons/circle-check';
	import { resolve } from '$app/paths';
	import { installAgentOverSSH, newAgentCode } from '#lib/api/client.js';
	import type { AgentCode, AgentRun, Device } from '#lib/api/types.js';
	import { timeOfDay } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import { toast } from '#lib/toast.svelte.js';
	import Button from '#lib/ui/Button.svelte';
	import type { ButtonSize, ButtonVariant } from '#lib/ui/button.js';
	import CodeLine from '#lib/ui/CodeLine.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	interface Props {
		/** The device to install the agent on, if it's known. */
		device?: Device;
		label?: string;
		variant?: ButtonVariant;
		size?: ButtonSize;
		icon?: Snippet;
		class?: string;
	}

	let {
		device,
		label = 'Add agent',
		variant = 'secondary',
		size = 'sm',
		icon,
		class: className
	}: Props = $props();

	let open = $state(false);
	let code = $state<AgentCode | null>(null);
	let error = $state<string | null>(null);
	/** What the install over SSH printed, once it ended. */
	let ran = $state<AgentRun | null>(null);
	let installing = $state(false);
	/** Counts the codes asked for, so an answer that comes late is dropped. */
	let asked = 0;

	// Roost can run the install on a device it's logged in to.
	const overSSH = $derived(device?.ssh?.state === 'ok' ? device.ssh : null);

	async function installOverSSH() {
		if (!device) {
			return;
		}
		installing = true;
		ran = null;
		try {
			ran = await installAgentOverSSH(device.id);
			if (ran.ok) {
				// The dialog may go once the device has an agent.
				toast(`Installed the agent on ${device.name}`);
			}
		} catch {
			// The client showed the error as a toast.
		} finally {
			installing = false;
		}
	}

	async function start() {
		open = true;
		code = null;
		error = null;
		ran = null;
		const n = ++asked;
		try {
			const c = await newAgentCode();
			if (n === asked) {
				code = c;
			}
		} catch (err) {
			if (n === asked) {
				error = err instanceof Error ? err.message : String(err);
			}
		}
	}

	const title = $derived.by(() => {
		if (!device) {
			return 'Add an agent';
		}
		if (device.agent?.outdated) {
			return `Update the agent on ${device.name}`;
		}
		return device.agent ? `Enroll ${device.name}'s agent again` : `Add the agent to ${device.name}`;
	});
	const description = $derived(
		`Run this on ${device ? device.name : 'the machine to add'}, on macOS or Linux. ` +
			'It installs the agent for the user who runs it, without root.'
	);

	/** The hub's clock, as of its latest snapshot. */
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());
	const madeAt = $derived(code ? Date.parse(code.createdAt) : NaN);
	const expired = $derived(code !== null && now >= Date.parse(code.expiresAt));
	/**
	 * The device that enrolled with the code, once one has: `device` if it
	 * was, since the machine Roost runs on can show up as two devices.
	 */
	const enrolled = $derived.by(() => {
		const isNew = (d: Device) => !!d.agent?.enrolledAt && Date.parse(d.agent.enrolledAt) >= madeAt;
		const devices = live.snapshot?.devices ?? [];
		return devices.find((d) => d.id === device?.id && isNew(d)) ?? devices.find(isNew);
	});
	const reporting = $derived(
		!!enrolled?.agent?.lastSeen && Date.parse(enrolled.agent.lastSeen) >= madeAt
	);
</script>

<Button {variant} {size} {icon} class={className} onclick={start}>{label}</Button>

{#snippet spinner()}
	<span
		class="size-3.5 animate-spin rounded-full border-2 border-current border-t-transparent text-fg-subtle"
		aria-hidden="true"
	></span>
{/snippet}

<Dialog bind:open {title} {description}>
	{#if error}
		<p class="text-[13px] text-red-fg">Couldn't make an install command: {error}</p>
	{:else if !code}
		<Skeleton class="h-10" />
	{:else}
		{#if device && overSSH}
			<div class="mb-4 space-y-2 rounded-lg border border-line bg-canvas p-3">
				<p class="text-[13px] text-fg-muted">
					Roost can log in to {device.name} as
					<span class="font-mono text-fg">{overSSH.user}</span>
					{#if overSSH.method === 'tailscale'}with Tailscale SSH{/if}
					and install it there, with no command to copy.
				</p>
				<Button variant="primary" loading={installing} onclick={installOverSSH}>
					{ran && !ran.ok ? 'Try again' : 'Install over SSH'}
				</Button>
				{#if ran}
					<p class={['text-[13px]', ran.ok ? 'text-green-fg' : 'text-red-fg']} role="status">
						{ran.ok ? 'The install ran.' : 'The install failed.'}
					</p>
					{#if ran.output}
						<pre
							class="max-h-48 overflow-auto rounded-md border border-line bg-surface px-3 py-2 font-mono text-[11.5px] whitespace-pre-wrap text-fg-muted">{ran.output}</pre>
					{/if}
				{/if}
			</div>
			<p class="mb-2 text-[12px] text-fg-subtle">Or run this on the machine yourself:</p>
		{/if}
		<CodeLine command={code.installCommand} />
		<p class="mt-2 text-[12px] text-fg-subtle">
			It works on one machine until {timeOfDay(code.expiresAt)}. If the install fails, fix what it
			says and run it again on the same machine.
			{#if device?.agent}
				The agent gets a new secret, and its old one stops working.
			{/if}
		</p>
		<div class="mt-4 flex items-center gap-2 text-[13px]" role="status">
			{#if enrolled && reporting}
				<CircleCheck class="size-4 text-green-fg" />
				<span class="text-fg">
					<a
						href={resolve('/devices/[id]', { id: enrolled.id })}
						class="font-medium underline decoration-line-heavy underline-offset-4"
						>{enrolled.name}</a
					> is reporting.
				</span>
			{:else if enrolled}
				{@render spinner()}
				<span class="text-fg-muted">{enrolled.name} enrolled. Waiting for its first heartbeat…</span
				>
			{:else if expired}
				<span class="text-fg-muted">This command expired.</span>
				<Button size="xs" onclick={start}>Make a new one</Button>
			{:else}
				{@render spinner()}
				<span class="text-fg-muted">Waiting for a machine to enroll…</span>
			{/if}
		</div>
	{/if}
	{#snippet footer()}
		<Button variant={enrolled && reporting ? 'primary' : 'ghost'} onclick={() => (open = false)}>
			{enrolled && reporting ? 'Done' : 'Close'}
		</Button>
	{/snippet}
</Dialog>
