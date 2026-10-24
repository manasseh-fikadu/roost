<!--
	How Roost logs in to a device over SSH, for terminals, files and Docker:
	how to set it up, and once it is, whether it works and what the machine
	offers. Only admins see it.
-->
<script lang="ts">
	import Ellipsis from '@lucide/svelte/icons/ellipsis';
	import KeyRound from '@lucide/svelte/icons/key-round';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import ShieldAlert from '@lucide/svelte/icons/shield-alert';
	import SquareTerminal from '@lucide/svelte/icons/square-terminal';
	import { resolve } from '$app/paths';
	import {
		authorizeSSH,
		checkSSH,
		getSSHSetup,
		putSSH,
		removeSSH,
		resetHostKey
	} from '#lib/api/client.js';
	import type { Device, DeviceSSH, SSHSetup } from '#lib/api/types.js';
	import { ago } from '#lib/format.js';
	import type { Status } from '#lib/status.js';
	import { tailnetIP } from '#lib/tailnet.js';
	import { canOpen, sshStateLabel } from '#lib/terminal.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Button from '#lib/ui/Button.svelte';
	import { buttonClass } from '#lib/ui/button.js';
	import Card from '#lib/ui/Card.svelte';
	import CodeLine from '#lib/ui/CodeLine.svelte';
	import DataRow from '#lib/ui/DataRow.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import MenuItem from '#lib/ui/MenuItem.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	interface Props {
		device: Device;
		/** The hub's clock. */
		now: number;
	}

	let { device, now }: Props = $props();

	// A prop's device is a new object with each snapshot, so effects read
	// its ID, which isn't.
	const deviceId = $derived(device.id);
	const ssh = $derived<DeviceSSH>(device.ssh ?? { state: 'not-set-up' });
	const setUp = $derived(ssh.state !== 'not-set-up');

	let editing = $state(false);
	let setup = $state<SSHSetup | null>(null);
	let setupError = $state<string | null>(null);
	let user = $state('');
	let port = $state('22');
	let busy = $state<string | null>(null);
	/** What the admin is asked to confirm, while confirmOpen. */
	let confirm = $state<'remove' | 'reset'>('remove');
	let confirmOpen = $state(false);

	function ask(what: 'remove' | 'reset') {
		confirm = what;
		confirmOpen = true;
	}

	// The setup command, while it's shown.
	$effect(() => {
		const id = deviceId;
		if (setUp && !editing) {
			return;
		}
		let current = true;
		setupError = null;
		getSSHSetup(id)
			.then((s) => {
				if (current) {
					setup = s;
				}
			})
			.catch((err: unknown) => {
				if (current) {
					setupError = err instanceof Error ? err.message : String(err);
				}
			});
		return () => {
			current = false;
		};
	});

	/** Runs an action, showing `name` as busy until it's done. */
	async function run(name: string, action: () => Promise<unknown>): Promise<boolean> {
		busy = name;
		try {
			await action();
			return true;
		} catch {
			// The client showed the error as a toast.
			return false;
		} finally {
			busy = null;
		}
	}

	function startEditing() {
		user = ssh.user ?? '';
		port = String(ssh.port ?? 22);
		editing = true;
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		const p = port.trim() === '' ? 22 : Number(port);
		if (await run('save', () => putSSH(deviceId, { user: user.trim(), port: p }))) {
			editing = false;
		}
	}

	async function confirmed() {
		const action = confirm === 'remove' ? () => removeSSH(deviceId) : () => resetHostKey(deviceId);
		if (await run('confirm', action)) {
			confirmOpen = false;
		}
	}

	const stateStatus: Record<DeviceSSH['state'], Status> = {
		'not-set-up': 'unknown',
		unchecked: 'unknown',
		ok: 'online',
		failing: 'offline',
		'host-key-changed': 'offline'
	};

	const address = $derived(ssh.hub ? '127.0.0.1' : (tailnetIP(device) ?? device.dnsName));
	const mac = $derived(device.os === 'macOS');
</script>

{#snippet setupForm(submit: string)}
	<form class="flex flex-wrap items-end gap-2" onsubmit={save}>
		<Field label="User">
			<Input
				bind:value={user}
				placeholder="alice"
				class="w-40"
				autocomplete="off"
				autocapitalize="off"
				spellcheck="false"
				required
			/>
		</Field>
		<Field label="Port">
			<Input bind:value={port} class="w-20" inputmode="numeric" placeholder="22" />
		</Field>
		<Button type="submit" variant="primary" size="md" class="h-9" loading={busy === 'save'}
			>{submit}</Button
		>
		{#if editing}
			<Button size="md" class="h-9" variant="ghost" onclick={() => (editing = false)}>Cancel</Button
			>
		{/if}
	</form>
{/snippet}

{#snippet steps()}
	{#if setupError}
		<p class="text-[13px] text-red-fg">Couldn't get Roost's key: {setupError}</p>
	{:else if !setup}
		<Skeleton class="h-24" />
	{:else}
		<ol class="space-y-4 text-[13px] text-fg-muted">
			{#if ssh.hub && setup.hubUser && !editing}
				<li class="space-y-2">
					<p>
						Roost logs in to this machine on its loopback address. It can add its key to the
						authorized_keys of <span class="font-mono text-fg">{setup.hubUser}</span>, the user it
						runs as.
					</p>
					<Button
						variant="primary"
						loading={busy === 'authorize'}
						onclick={() => run('authorize', () => authorizeSSH(deviceId))}
					>
						{#snippet icon()}<KeyRound class="size-3.5" />{/snippet}
						Authorize Roost as {setup.hubUser}
					</Button>
				</li>
				<li class="border-t border-line pt-4">To log in as another user, run this as that user:</li>
			{:else}
				<li>
					<span class="mr-1.5 font-mono text-fg-faint">1</span>
					Run this on {device.name}, as the user Roost should log in as:
				</li>
			{/if}
			<li class="space-y-1.5">
				<CodeLine command={setup.command} />
				<p class="text-[12px] text-fg-subtle">
					The key only works from Roost's own addresses, without agent or X11 forwarding.
					{#if mac}
						On a Mac, turn on Remote Login first, in System Settings → General → Sharing.
					{/if}
				</p>
			</li>
			<li class="space-y-2">
				<p>
					{#if !(ssh.hub && setup.hubUser) || editing}
						<span class="mr-1.5 font-mono text-fg-faint">2</span>
					{/if}
					Then tell Roost which user that was.
				</p>
				{@render setupForm(editing ? 'Save' : 'Connect')}
			</li>
		</ol>
	{/if}
{/snippet}

<div id="ssh" class="scroll-mt-24">
	<Card
		title="SSH access"
		description="Terminals, files and Docker on this machine go over SSH, with Roost's own key."
	>
		{#snippet actions()}
			{#if setUp && !editing}
				<Menu>
					{#snippet trigger({ props })}
						<IconButton label="SSH access options" size="sm" {...props}>
							<Ellipsis class="size-4" />
						</IconButton>
					{/snippet}
					<MenuItem onSelect={startEditing}>Change user or port</MenuItem>
					{#if ssh.hostKey}
						<MenuItem onSelect={() => ask('reset')}>Reset the host key</MenuItem>
					{/if}
					<MenuItem danger onSelect={() => ask('remove')}>Remove access</MenuItem>
				</Menu>
			{/if}
		{/snippet}

		{#if !setUp || editing}
			<div class="p-4">{@render steps()}</div>
		{:else}
			{#if ssh.state === 'host-key-changed'}
				<div class="space-y-2 border-b border-red/30 bg-red/5 px-4 py-3 text-[13px]">
					<p class="flex items-center gap-2 font-medium text-red-fg">
						<ShieldAlert class="size-4" /> The host key changed
					</p>
					<p class="text-fg-muted">
						Roost won't log in until you trust the new key. If {device.name} wasn't reinstalled, another
						machine may be answering for it.
					</p>
					<dl class="space-y-0.5 font-mono text-[11.5px] break-all">
						<div>
							<dt class="inline text-fg-subtle">Pinned</dt>
							<dd class="inline text-fg">{ssh.hostKey}</dd>
						</div>
						<div>
							<dt class="inline text-fg-subtle">Offered</dt>
							<dd class="inline text-fg">{ssh.offeredKey}</dd>
						</div>
					</dl>
					<Button size="xs" onclick={() => ask('reset')}>Trust the new key</Button>
				</div>
			{/if}
			<dl>
				<DataRow label="Status">
					<span class="flex min-w-0 items-center gap-2" title={ssh.error}>
						<StatusDot status={stateStatus[ssh.state]} />
						<span class={['truncate', ssh.state === 'failing' && 'text-red-fg']}>
							{ssh.state === 'failing' && ssh.error ? ssh.error : sshStateLabel(ssh)}
						</span>
					</span>
				</DataRow>
				<DataRow label="Login" copy="{ssh.user}@{address}">{ssh.user}@{address}:{ssh.port}</DataRow>
				<DataRow label="Host key" copy={ssh.hostKey}>
					{ssh.hostKey ? `${ssh.hostKey.slice(0, 22)}…` : 'pinned at the first login'}
				</DataRow>
				<DataRow label="tmux">
					{#if ssh.tmux === undefined}
						<span class="text-fg-faint">unknown</span>
					{:else if ssh.tmux}
						<Badge tone="green">sessions survive a closed tab</Badge>
					{:else}
						<span class="text-fg-subtle">not installed</span>
					{/if}
				</DataRow>
				<DataRow label="Docker">
					{#if ssh.docker === undefined}
						<span class="text-fg-faint">unknown</span>
					{:else if ssh.docker}
						<Badge tone="green">reachable</Badge>
					{:else}
						<span class="text-fg-subtle">no socket {ssh.user} can open</span>
					{/if}
				</DataRow>
				<DataRow label="Checked">{ssh.checkedAt ? ago(ssh.checkedAt, now) : 'not yet'}</DataRow>
			</dl>
			<div class="flex flex-wrap items-center gap-2 border-t border-line p-3">
				{#if canOpen(device)}
					<a
						href="{resolve('/terminal')}?device={encodeURIComponent(device.id)}"
						class={buttonClass('secondary', 'sm')}
					>
						<SquareTerminal class="size-3.5" /> Terminal
					</a>
				{/if}
				<Button
					variant="ghost"
					class="ml-auto"
					loading={busy === 'check'}
					onclick={() => run('check', () => checkSSH(deviceId))}
				>
					{#snippet icon()}<RefreshCw class="size-3.5" />{/snippet}
					Check
				</Button>
			</div>
		{/if}
	</Card>
</div>

<Dialog
	bind:open={confirmOpen}
	title={confirm === 'remove' ? `Remove SSH access to ${device.name}?` : 'Trust the next host key?'}
>
	<p class="text-[13px] text-fg-muted">
		{#if confirm === 'remove'}
			Roost closes its connections and terminals there. Its key stays in the machine's
			authorized_keys until you take it out.
		{:else}
			Roost forgets the key it pinned for {device.name}, and trusts the one the machine offers at
			the next login. Only do this if you know why the key changed, such as a reinstall.
		{/if}
	</p>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (confirmOpen = false)}>Cancel</Button>
		<Button
			variant={confirm === 'remove' ? 'danger' : 'primary'}
			loading={busy === 'confirm'}
			onclick={confirmed}
		>
			{confirm === 'remove' ? 'Remove' : 'Reset the host key'}
		</Button>
	{/snippet}
</Dialog>
