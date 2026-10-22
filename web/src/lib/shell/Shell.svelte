<!--
	The app around every page: the sidebar (a drawer on small screens), the
	header with the live status, search and the signed-in user, the command
	palette and the toasts. It keeps the live stream connected while it's
	shown.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Dialog } from 'bits-ui';
	import LockKeyhole from '@lucide/svelte/icons/lock-keyhole';
	import MenuIcon from '@lucide/svelte/icons/menu';
	import Search from '@lucide/svelte/icons/search';
	import { afterNavigate, invalidate } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { isAdmin, lookupFailed } from '#lib/access.js';
	import { getMe } from '#lib/api/client.js';
	import { connect, live } from '#lib/live.svelte.js';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Kbd from '#lib/ui/Kbd.svelte';
	import Toaster from '#lib/ui/Toaster.svelte';
	import { iconButtonClass } from '#lib/ui/button.js';
	import CommandPalette from './CommandPalette.svelte';
	import LiveIndicator from './LiveIndicator.svelte';
	import Sidebar from './Sidebar.svelte';
	import UserMenu from './UserMenu.svelte';
	import { navItemFor } from './nav.js';

	interface Props {
		children: Snippet;
	}

	let { children }: Props = $props();

	/** Macs say ⌘ for the palette's shortcut, and everything else Ctrl. */
	const modKey = /Mac|iPhone|iPad/.test(navigator.userAgent) ? '⌘' : 'Ctrl';

	let drawerOpen = $state(false);
	let paletteOpen = $state(false);

	const item = $derived(navItemFor(page.url.pathname));
	/** A device's page is named after the device, which only the live data knows. */
	const deviceName = $derived(
		page.route.id === '/devices/[id]'
			? live.snapshot?.devices.find((d) => d.id === page.params.id)?.name
			: undefined
	);
	const title = $derived(
		page.data.title ??
			deviceName ??
			(page.error ? (page.status === 404 ? 'Not found' : 'Error') : item?.label) ??
			'Roost'
	);
	/** The page is for admins, and the caller isn't one. */
	const forbidden = $derived(item?.adminOnly === true && !isAdmin(page.data.me));

	afterNavigate(() => {
		drawerOpen = false;
	});

	$effect(() => connect({ onDrop: checkRole }));

	/**
	 * Asks the hub again who the caller is, and reloads `me` if their role
	 * changed, so the sidebar and pages follow. The hub ends the stream when
	 * a role changes. An answer that Roost couldn't find out their role isn't
	 * a change: the next drop asks again.
	 */
	async function checkRole() {
		try {
			const me = await getMe();
			if (me.role !== page.data.me?.role && !lookupFailed(me)) {
				await invalidate('roost:me');
			}
		} catch {
			// The hub is down or restarting. The stream keeps trying.
		}
	}
</script>

<svelte:head>
	<title>{title === 'Roost' ? title : `${title} · Roost`}</title>
</svelte:head>

<div class="min-h-dvh">
	<aside class="fixed inset-y-0 left-0 z-30 hidden w-60 border-r border-line bg-canvas lg:block">
		<Sidebar />
	</aside>

	<Dialog.Root bind:open={drawerOpen}>
		<Dialog.Portal>
			<Dialog.Overlay class="fixed inset-0 z-40 bg-black/70 fade lg:hidden" />
			<Dialog.Content
				class="fixed inset-y-0 left-0 z-40 w-64 border-r border-line bg-canvas transition-[translate] duration-200 ease-out outline-none data-[ending-style]:-translate-x-full data-[starting-style]:-translate-x-full lg:hidden"
			>
				<Dialog.Title class="sr-only">Pages</Dialog.Title>
				<Sidebar />
			</Dialog.Content>
		</Dialog.Portal>
	</Dialog.Root>

	<div class="lg:pl-60">
		<header
			class="sticky top-0 z-20 flex h-14 items-center gap-3 border-b border-line bg-canvas/80 px-4 backdrop-blur-md sm:px-6"
		>
			<button
				type="button"
				class={[iconButtonClass(), '-ml-1 lg:hidden']}
				onclick={() => (drawerOpen = true)}
				aria-label="Open the page list"
			>
				<MenuIcon class="size-4" />
			</button>
			<nav class="flex min-w-0 items-center gap-2 text-[14px]" aria-label="Breadcrumb">
				<a
					href={resolve('/')}
					class="hidden text-fg-faint transition-colors hover:text-fg-muted sm:inline">Roost</a
				>
				<span class="hidden text-line-heavy sm:inline" aria-hidden="true">/</span>
				<span class="truncate font-medium text-fg" aria-current="page">{title}</span>
			</nav>
			<div class="ml-auto flex items-center gap-3">
				<LiveIndicator />
				<button
					type="button"
					onclick={() => (paletteOpen = true)}
					class="flex h-8 items-center gap-2 rounded-md border border-line-strong bg-surface px-2.5 text-[13px] text-fg-subtle transition-colors hover:border-line-heavy hover:text-fg-muted"
					aria-label="Search pages and devices"
					aria-keyshortcuts="Meta+K Control+K"
				>
					<Search class="size-3.5" />
					<span class="hidden sm:inline">Search</span>
					<span class="hidden items-center gap-0.5 sm:flex" aria-hidden="true">
						<Kbd>{modKey}</Kbd>
						<Kbd>K</Kbd>
					</span>
				</button>
				<UserMenu />
			</div>
		</header>
		<main class="mx-auto max-w-[1400px] px-4 py-6 sm:px-6 lg:py-8">
			{#if forbidden}
				<EmptyState title="Only admins can open {title}" icon={LockKeyhole}>
					An admin can make you one with <code class="font-mono text-fg-muted">roost admin add</code
					>.
				</EmptyState>
			{:else}
				{@render children()}
			{/if}
		</main>
	</div>

	<CommandPalette bind:open={paletteOpen} />
	<Toaster />
</div>
