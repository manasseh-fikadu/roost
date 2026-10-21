<!-- Who's signed in, as an avatar that opens a menu with their details. -->
<script lang="ts">
	import Bug from '@lucide/svelte/icons/bug';
	import { page } from '$app/state';
	import { isAdmin, roleLabel } from '#lib/access.js';
	import { live } from '#lib/live.svelte.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import MenuItem from '#lib/ui/MenuItem.svelte';
	import MenuLabel from '#lib/ui/MenuLabel.svelte';
	import MenuSeparator from '#lib/ui/MenuSeparator.svelte';

	const me = $derived(page.data.me);
	const name = $derived(me?.name || me?.login || 'Signed in');
	const role = $derived(me?.role ? roleLabel(me.role) : '');
</script>

<Menu class="w-64">
	{#snippet trigger({ props })}
		<button
			{...props}
			type="button"
			class="flex items-center gap-2 rounded-md py-0.5 pr-0.5 pl-1.5 transition-colors hover:bg-hover"
			aria-label="Signed in as {name}"
		>
			<span class="hidden text-right lg:block">
				<span class="block max-w-40 truncate text-[12px] leading-4 text-fg-muted">{name}</span>
				<span class="block font-mono text-[10px] leading-3 text-fg-faint">{role}</span>
			</span>
			<span
				class="flex size-7 items-center justify-center rounded-full border border-line-strong bg-gradient-to-br from-[#2a2a2a] to-raised text-[11px] font-medium text-fg-muted"
				aria-hidden="true"
			>
				{name.slice(0, 1).toUpperCase()}
			</span>
		</button>
	{/snippet}
	<MenuLabel>
		<span class="flex items-center justify-between gap-2">
			<span class="truncate text-[13px] font-medium text-fg">{name}</span>
			{#if role}
				<Badge>{role}</Badge>
			{/if}
		</span>
		{#if me?.login && me.login !== name}
			<span class="mt-0.5 block truncate">{me.login}</span>
		{/if}
		{#if me?.device}
			<span class="mt-0.5 block truncate font-mono text-[11px] text-fg-faint">from {me.device}</span
			>
		{/if}
	</MenuLabel>
	{#if isAdmin(me)}
		<MenuSeparator />
		<!-- An API URL, not an app route, so the browser loads it, not SvelteKit's router. -->
		<MenuItem href="/api/debug/tailnet" reload icon={Bug}>Tailnet debug info</MenuItem>
	{/if}
	{#if live.snapshot}
		<MenuSeparator />
		<MenuLabel>
			<span class="font-mono text-[11px] text-fg-faint">Roost {live.snapshot.hub.version}</span>
		</MenuLabel>
	{/if}
</Menu>
