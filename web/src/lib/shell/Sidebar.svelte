<!-- The sidebar: Roost's name, the pages the caller can open, and how the hub's collectors are doing. -->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { isAdmin } from '#lib/access.js';
	import { live } from '#lib/live.svelte.js';
	import type { Status } from '#lib/status.js';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import Logo from './Logo.svelte';
	import { navFor, navItemFor } from './nav.js';

	const sections = $derived.by(() => {
		let n = 0;
		return navFor(isAdmin(page.data.me)).map((s) => ({
			...s,
			items: s.items.map((item) => ({ ...item, number: String(++n).padStart(2, '0') }))
		}));
	});
	const current = $derived(navItemFor(page.url.pathname));

	const collectors = $derived(live.snapshot?.collectors ?? []);
	const healthy = $derived(collectors.filter((c) => c.state === 'ok').length);
	const collectorStatus: Status = $derived(
		collectors.some((c) => c.state === 'failing')
			? 'attention'
			: collectors.length && healthy === collectors.length
				? 'online'
				: 'unknown'
	);
</script>

<div class="flex h-full flex-col">
	<a href={resolve('/')} class="flex h-14 shrink-0 items-center gap-2.5 px-4">
		<span
			class="flex size-7 items-center justify-center rounded-md border border-line-strong bg-raised text-fg"
		>
			<Logo class="size-4" />
		</span>
		<span class="min-w-0">
			<span class="block truncate text-[14px] leading-4 font-semibold tracking-[-0.01em]"
				>{live.name}</span
			>
			<span
				class="block truncate font-mono text-[10px] leading-4 tracking-wider text-fg-faint uppercase"
			>
				Control plane
			</span>
		</span>
	</a>
	<nav class="flex-1 overflow-y-auto px-2 pt-2 pb-4" aria-label="Pages">
		{#each sections as section (section.label)}
			<div class="mb-4">
				<div class="px-2 pb-1.5 font-mono text-[10.5px] tracking-wider text-fg-faint uppercase">
					{section.label}
				</div>
				{#each section.items as item (item.href)}
					{@const active = item.href === current?.href}
					<a
						href={resolve(item.href)}
						aria-current={active ? 'page' : undefined}
						class={[
							'group flex h-8 items-center gap-2.5 rounded-md px-2 text-[14px] transition-colors',
							active ? 'bg-hover text-fg' : 'text-fg-subtle hover:bg-hover/60 hover:text-fg'
						]}
					>
						<span class="w-4 num text-[10.5px] text-fg-faint" aria-hidden="true">{item.number}</span
						>
						<item.icon
							class={[
								'size-4 transition-colors',
								active ? 'text-fg' : 'text-fg-faint group-hover:text-fg-muted'
							]}
						/>
						<span class="flex-1 truncate">{item.label}</span>
					</a>
				{/each}
			</div>
		{/each}
	</nav>
	<div class="border-t border-line p-3">
		<a
			href={resolve('/activity')}
			class="block rounded-md px-2 py-1.5 transition-colors hover:bg-hover/60"
		>
			<span class="flex items-center gap-2 text-[12px] text-fg-muted">
				<StatusDot status={collectorStatus} />
				{collectors.length ? `${healthy}/${collectors.length} collectors healthy` : 'Connecting…'}
			</span>
			<span class="mt-0.5 block truncate pl-4 font-mono text-[10.5px] text-fg-faint">
				{live.snapshot?.hub.dnsName ?? ''}
			</span>
		</a>
	</div>
</div>
