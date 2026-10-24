<script lang="ts">
	import ChevronDown from '@lucide/svelte/icons/chevron-down';
	import Layers from '@lucide/svelte/icons/layers';
	import Plus from '@lucide/svelte/icons/plus';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import ShieldAlert from '@lucide/svelte/icons/shield-alert';
	import SquareTerminal from '@lucide/svelte/icons/square-terminal';
	import X from '@lucide/svelte/icons/x';
	import { untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { getTerminalSessions, killTerminalSession } from '#lib/api/client.js';
	import type { Device, TerminalSessions } from '#lib/api/types.js';
	import { ago, plural } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import {
		canOpen,
		extraKeys,
		loadTabs,
		nextSessionName,
		saveTabs,
		tabsKey,
		terminalDevices,
		type TermStatus,
		type TerminalTab
	} from '#lib/terminal.js';
	import TerminalView from '#lib/terminal/TerminalView.svelte';
	import Badge from '#lib/ui/Badge.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import MenuItem from '#lib/ui/MenuItem.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	const all = $derived(live.snapshot?.devices ?? []);
	const devices = $derived(terminalDevices(all));
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());
	/** The device in ?device=, or else the machine Roost runs on, or else the first. */
	const device = $derived.by((): Device | undefined => {
		const wanted = page.url.searchParams.get('device');
		return devices.find((d) => d.id === wanted) ?? devices.find((d) => d.ssh?.hub) ?? devices[0];
	});
	const deviceId = $derived(device?.id);
	const ready = $derived(!!device && canOpen(device));
	const tmux = $derived(device?.ssh?.tmux !== false);

	function setDevice(id: string) {
		const url = new URL(page.url.href);
		url.searchParams.set('device', id);
		goto(url, { replace: true, reset: false });
	}

	let tabs = $state<TerminalTab[]>([]);
	let activeKey = $state<string | null>(null);
	let statuses = $state<Record<string, TermStatus>>({});
	let titles = $state<Record<string, string>>({});
	const views: Record<string, TerminalView> = {};
	/** Whether the tabs saved in localStorage have been read, once the devices are known. */
	let restored = $state(false);

	$effect(() => {
		if (restored || !live.snapshot) {
			return;
		}
		restored = true;
		tabs = loadTabs(
			localStorage.getItem(tabsKey),
			untrack(() => all)
		);
		activeKey =
			tabs.find((t) => t.deviceId === untrack(() => deviceId))?.key ?? tabs[0]?.key ?? null;
	});
	$effect(() => {
		if (restored) {
			localStorage.setItem(tabsKey, saveTabs(tabs));
		}
	});

	let sessions = $state<TerminalSessions | null>(null);
	let sessionsOpen = $state(false);
	/** The session an admin is about to end. */
	let ending = $state<string | null>(null);

	/** Lists the selected device's tmux sessions. */
	async function loadSessions() {
		const id = deviceId;
		if (!id || !ready || !tmux) {
			sessions = { tmux: false, sessions: [] };
			return;
		}
		try {
			const s = await getTerminalSessions(id);
			if (id === deviceId) {
				sessions = s;
			}
		} catch {
			if (id === deviceId) {
				sessions = { tmux: false, sessions: [] };
			}
		}
	}

	$effect(() => {
		// Again for each device, and once it can open terminals.
		void [deviceId, ready, tmux];
		sessions = null;
		untrack(loadSessions);
	});

	let seq = 0;
	const newKey = () => `t${Date.now().toString(36)}${(seq++).toString(36)}`;

	/** Opens a tab on a device: a tmux session, a new one by default, or with `plain`, a shell. */
	function open(id: string, opts: { session?: string; plain?: boolean; cwd?: string | null } = {}) {
		if (opts.session) {
			const existing = tabs.find((t) => t.deviceId === id && t.session === opts.session);
			if (existing) {
				activeKey = existing.key;
				return;
			}
		}
		let session: string | null = null;
		if (!opts.plain) {
			session =
				opts.session ??
				nextSessionName([
					...(id === deviceId ? (sessions?.sessions.map((s) => s.name) ?? []) : []),
					...tabs.filter((t) => t.deviceId === id && t.session).map((t) => t.session as string)
				]);
		}
		const tab = { key: newKey(), deviceId: id, session, cwd: opts.cwd ?? null };
		tabs.push(tab);
		activeKey = tab.key;
	}

	function close(key: string) {
		const i = tabs.findIndex((t) => t.key === key);
		if (i < 0) {
			return;
		}
		tabs.splice(i, 1);
		delete statuses[key];
		delete titles[key];
		delete views[key];
		if (activeKey === key) {
			activeKey = tabs[Math.min(i, tabs.length - 1)]?.key ?? null;
		}
		// The session is detached by now.
		setTimeout(loadSessions, 600);
	}

	async function end(name: string) {
		const id = deviceId;
		if (!id) {
			return;
		}
		try {
			await killTerminalSession(id, name);
		} catch {
			// The client showed the error as a toast.
			return;
		} finally {
			ending = null;
		}
		for (const t of tabs.filter((t) => t.deviceId === id && t.session === name)) {
			close(t.key);
		}
		await loadSessions();
	}

	/**
	 * The device a link opened the page for, such as the Terminal button on
	 * a device's page, until a tab is open on it.
	 */
	let linkedTo = $state(page.url.searchParams.get('device'));

	$effect(() => {
		const id = linkedTo;
		if (
			!id ||
			!restored ||
			!ready ||
			deviceId !== id ||
			sessions === null ||
			page.url.searchParams.has('cwd')
		) {
			return;
		}
		linkedTo = null;
		untrack(() => {
			const tab = tabs.find((t) => t.deviceId === id);
			if (tab) {
				activeKey = tab.key;
				return;
			}
			// Reattach the latest session a tab here left, or start one.
			const left = sessions?.sessions
				.filter((s) => s.name.startsWith('web-') && s.attached === 0)
				.sort((a, b) => Date.parse(b.createdAt ?? '0') - Date.parse(a.createdAt ?? '0'))[0];
			open(id, left ? { session: left.name } : { plain: !sessions?.tmux });
		});
	});

	// ?cwd=, from the file manager's "Open a terminal here", opens a tab there once.
	$effect(() => {
		const cwd = page.url.searchParams.get('cwd');
		if (!cwd || !restored || !ready || !deviceId || sessions === null) {
			return;
		}
		untrack(() => open(deviceId, { plain: !sessions?.tmux, cwd }));
		const url = new URL(page.url.href);
		url.searchParams.delete('cwd');
		goto(url, { replace: true, reset: false });
	});

	const active = $derived(tabs.find((t) => t.key === activeKey));
	const activeStatus = $derived(active ? statuses[active.key] : undefined);
	const manyDevices = $derived(new Set(tabs.map((t) => t.deviceId)).size > 1);
	const openNames = $derived(
		new Set(tabs.filter((t) => t.deviceId === deviceId).map((t) => t.session))
	);
	const nameOf = (id: string) => all.find((d) => d.id === id)?.name ?? id;

	const dots: Record<TermStatus['state'], 'online' | 'unknown' | 'offline'> = {
		open: 'online',
		connecting: 'unknown',
		closed: 'offline',
		error: 'offline'
	};

	/** Fills the window below the terminal's top edge, so the page doesn't scroll. */
	let frame = $state<HTMLElement>();
	let height = $state(520);
	$effect(() => {
		const el = frame;
		if (!el) {
			return;
		}
		const measure = () => {
			height = Math.max(
				380,
				window.innerHeight - el.getBoundingClientRect().top - window.scrollY - 24
			);
		};
		measure();
		window.addEventListener('resize', measure);
		return () => window.removeEventListener('resize', measure);
	});
</script>

{#snippet plusIcon()}<Plus class="size-3.5" />{/snippet}
{#snippet layersIcon()}<Layers class="size-3.5" />{/snippet}
{#snippet refreshIcon()}<RefreshCw class="size-3.5" />{/snippet}

<AdminOnly>
	<PageHeader
		title="Terminal"
		description="A shell on any machine Roost has SSH access to. With tmux, it keeps running after you close the tab."
	>
		{#snippet actions()}
			{#if device}
				{#if devices.length > 1}
					<Select
						value={device.id}
						onchange={(e) => setDevice(e.currentTarget.value)}
						class="h-8 w-52 text-[13px]"
						aria-label="Device"
					>
						{#each devices as d (d.id)}
							<option value={d.id}>{d.name}{d.online || d.ssh?.hub ? '' : ' (offline)'}</option>
						{/each}
					</Select>
				{/if}
				{#if ready && sessions?.tmux}
					<Button icon={layersIcon} onclick={() => ((sessionsOpen = true), loadSessions())}>
						Sessions
						{#if sessions.sessions.length > 0}
							<span class="num text-fg-subtle">{sessions.sessions.length}</span>
						{/if}
					</Button>
				{/if}
				{#if ready}
					{@const id = device.id}
					<Menu>
						{#snippet trigger({ props })}
							<Button {...props} variant="primary" icon={plusIcon}>
								New <ChevronDown class="size-3.5" />
							</Button>
						{/snippet}
						{#if tmux}
							<MenuItem icon={Layers} onSelect={() => open(id)}>tmux session</MenuItem>
						{/if}
						<MenuItem icon={SquareTerminal} onSelect={() => open(id, { plain: true })}>
							Shell that ends with its tab
						</MenuItem>
					</Menu>
				{/if}
			{/if}
		{/snippet}
	</PageHeader>

	{#if !live.snapshot}
		<Skeleton class="h-96 rounded-xl" />
	{:else if !device}
		<Card>
			<EmptyState title="No machine has SSH access yet" icon={SquareTerminal}>
				Open a device, and set up Roost's access in its SSH access card. On the machine Roost runs
				on, it takes one click.
				<a
					href={resolve('/devices')}
					class="mt-2 block text-fg-muted underline decoration-line-heavy underline-offset-4 hover:text-fg"
					>See the devices</a
				>
			</EmptyState>
		</Card>
	{:else if tabs.length === 0}
		<Card>
			{#if ready}
				<EmptyState title="No terminal open on {device.name}" icon={SquareTerminal}>
					<Button
						class="mt-3"
						variant="primary"
						icon={plusIcon}
						onclick={() => open(device.id, { plain: !tmux })}>Open a terminal</Button
					>
				</EmptyState>
			{:else}
				<EmptyState
					title={device.ssh?.state === 'host-key-changed'
						? `${device.name}'s host key changed`
						: `${device.name} is offline`}
					icon={ShieldAlert}
				>
					{#if device.ssh?.state === 'host-key-changed'}
						Roost won't log in until an admin trusts the new key, on <a
							href="{resolve('/devices/[id]', { id: device.id })}#ssh"
							class="text-fg underline underline-offset-4">its page</a
						>.
					{:else}
						Terminals open again once it's back on the tailnet.
					{/if}
				</EmptyState>
			{/if}
		</Card>
	{:else}
		<section bind:this={frame} class="flex flex-col overflow-hidden card" style:height="{height}px">
			<div class="flex items-stretch border-b border-line bg-canvas/40">
				<div class="flex min-w-0 flex-1 overflow-x-auto" role="tablist" aria-label="Terminals">
					{#each tabs as tab (tab.key)}
						{@const isActive = tab.key === activeKey}
						<div
							class={[
								'group flex h-10 shrink-0 items-center gap-1 border-r border-line pr-1 pl-3 text-[13px] transition-colors',
								isActive ? 'bg-surface text-fg' : 'text-fg-subtle hover:bg-hover/50 hover:text-fg'
							]}
						>
							<button
								type="button"
								role="tab"
								aria-selected={isActive}
								class="flex items-center gap-2"
								title={titles[tab.key]}
								onclick={() => (activeKey = tab.key)}
							>
								<StatusDot status={dots[statuses[tab.key]?.state ?? 'connecting']} />
								<span class="font-mono text-[12.5px]">{tab.session ?? 'shell'}</span>
								{#if manyDevices}
									<span class="text-[11.5px] text-fg-faint">{nameOf(tab.deviceId)}</span>
								{/if}
							</button>
							<IconButton
								label={tab.session ? 'Close the tab; the session keeps running' : 'Close the tab'}
								size="sm"
								class="size-6 opacity-60 group-hover:opacity-100"
								onclick={() => close(tab.key)}
							>
								<X class="size-3.5" />
							</IconButton>
						</div>
					{/each}
				</div>
				{#if active}
					<div
						class="hidden shrink-0 items-center gap-3 px-3 font-mono text-[11px] text-fg-faint sm:flex"
					>
						<span class="max-w-64 truncate" title={titles[active.key]}>
							{titles[active.key] ?? nameOf(active.deviceId)}
						</span>
						<span>{active.session ? 'tmux · Shift-drag selects' : 'ends with the tab'}</span>
					</div>
				{/if}
			</div>

			<div class="relative min-h-0 flex-1 bg-surface">
				{#each tabs as tab (tab.key)}
					<div class={['absolute inset-0 p-2 pr-0', tab.key !== activeKey && 'invisible']}>
						<TerminalView
							bind:this={views[tab.key]}
							deviceId={tab.deviceId}
							session={tab.session}
							cwd={tab.cwd}
							active={tab.key === activeKey}
							onstatus={(s) => (statuses[tab.key] = s)}
							ontitle={(t) => (titles[tab.key] = t)}
						/>
					</div>
				{/each}
				{#if active && activeStatus && (activeStatus.state === 'closed' || activeStatus.state === 'error')}
					{@const key = active.key}
					<div
						class="absolute inset-x-0 bottom-0 z-10 flex flex-wrap items-center justify-between gap-3 border-t border-line-strong bg-raised px-4 py-3"
						role="status"
					>
						<span
							class={[
								'text-[13px]',
								activeStatus.state === 'error' ? 'text-red-fg' : 'text-fg-muted'
							]}
						>
							{activeStatus.message ?? 'Disconnected.'}
						</span>
						<span class="flex gap-2">
							<Button size="xs" icon={refreshIcon} onclick={() => views[key]?.reconnect()}>
								{active.session ? 'Reattach' : 'New shell'}
							</Button>
							<Button size="xs" variant="ghost" onclick={() => close(key)}>Close the tab</Button>
						</span>
					</div>
				{/if}
			</div>

			<div class="flex gap-1 overflow-x-auto border-t border-line bg-canvas/40 p-1.5 md:hidden">
				{#each extraKeys as k (k.label)}
					<button
						type="button"
						class="h-8 min-w-10 shrink-0 rounded-md border border-line-strong bg-surface px-2 font-mono text-[12px] text-fg-muted active:bg-hover"
						onpointerdown={(e) => e.preventDefault()}
						onclick={() => active && views[active.key]?.send(k.data)}
					>
						{k.label}
					</button>
				{/each}
			</div>
		</section>
	{/if}

	{#snippet fallback()}
		<PageHeader title="Terminal" />
		<Card>
			<EmptyState title="Only admins can open terminals" icon={SquareTerminal} />
		</Card>
	{/snippet}
</AdminOnly>

<Dialog
	bind:open={sessionsOpen}
	title="tmux sessions on {device?.name ?? 'this machine'}"
	description="They keep running when their tab closes. Attach to one in a tab, or end it and what runs in it."
>
	{#if sessions === null}
		<Skeleton class="h-16" />
	{:else if sessions.sessions.length === 0}
		<p class="text-[13px] text-fg-subtle">No sessions are running.</p>
	{:else}
		<ul class="-mx-2">
			{#each sessions.sessions as s (s.name)}
				<li class="flex items-center gap-3 rounded-md px-2 py-2 hover:bg-hover/50">
					<StatusDot status={s.attached > 0 ? 'online' : 'unknown'} />
					<div class="min-w-0 flex-1">
						<p class="flex items-center gap-2 truncate font-mono text-[13px] text-fg">
							{s.name}
							{#if openNames.has(s.name)}<Badge>open here</Badge>{/if}
						</p>
						<p class="truncate text-[12px] text-fg-subtle">
							{s.command || 'shell'} · {s.attached > 0
								? `${plural(s.attached, 'client')} attached`
								: 'detached'}
							{#if s.createdAt}· started {ago(s.createdAt, now)}{/if}
						</p>
					</div>
					{#if ending === s.name}
						<Button size="xs" variant="danger" onclick={() => end(s.name)}>End it</Button>
						<Button size="xs" variant="ghost" onclick={() => (ending = null)}>Keep</Button>
					{:else}
						<Button
							size="xs"
							onclick={() => {
								if (deviceId) {
									open(deviceId, { session: s.name });
									sessionsOpen = false;
								}
							}}>Attach</Button
						>
						<Button size="xs" variant="ghost" onclick={() => (ending = s.name)}>End</Button>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</Dialog>
