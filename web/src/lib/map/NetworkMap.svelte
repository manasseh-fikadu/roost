<!--
	The network map: the internet and Tailscale's DERP relays above the hub,
	and every peer below it, with a line from the hub to each. A line flows
	while the hub can see the link, and is dashed while it can't. On a phone,
	the map is a list instead.
-->
<script lang="ts">
	import Globe from '@lucide/svelte/icons/globe';
	import RadioTower from '@lucide/svelte/icons/radio-tower';
	import { resolve } from '$app/paths';
	import type { Device, Snapshot } from '#lib/api/types.js';
	import { deviceIcon } from '#lib/device.js';
	import { dateTime, duration, latency, osLabel, percent, rateShort, ratio } from '#lib/format.js';
	import {
		derpStatus,
		linkLabel,
		pathLabel,
		slowLink,
		tailnetIP,
		uplinkStatus
	} from '#lib/tailnet.js';
	import MapCard from './MapCard.svelte';
	import MapFact from './MapFact.svelte';
	import { edgeState, layoutMap, type EdgeState, type Group, type MapEdge } from './layout.js';

	interface Props {
		snapshot: Snapshot;
	}

	let { snapshot }: Props = $props();

	/** Narrower than this, the map is a list. */
	const listBelow = 560;
	const upstreamHeight = 90;
	const deviceHeight = 132;

	/** What a card on the map shows. */
	type View = { kind: 'internet' } | { kind: 'derp' } | { kind: 'device'; device: Device };

	interface Node {
		id: string;
		h: number;
		view: View;
	}

	interface MapGroup extends Group {
		nodes: Node[];
		/** The heading over the group in the list. */
		title: string;
	}

	let width = $state(0);

	const hub = $derived(snapshot.devices.find((d) => d.self));
	const uplink = $derived(snapshot.uplink);

	function peerEdge(d: Device): MapEdge {
		let state: EdgeState = 'live';
		if (!d.online) {
			state = 'down';
		} else if (slowLink(d)) {
			state = 'warn';
		}
		return { to: d.id, label: pathLabel(d), state };
	}

	const groups: MapGroup[] = $derived.by(() => {
		const derp = uplink.derp;
		const out: MapGroup[] = [
			{
				id: 'upstream',
				title: 'Upstream',
				link: 'up',
				nodes: [
					{ id: 'internet', h: upstreamHeight, view: { kind: 'internet' } },
					{ id: 'derp', h: upstreamHeight, view: { kind: 'derp' } }
				],
				edges: [
					{ to: 'internet', label: 'uplink', state: edgeState(uplinkStatus(uplink)) },
					{
						to: 'derp',
						label: derp ? `${derp.code} · ${latency(derp.latencyMs)}` : 'netcheck',
						state: edgeState(derpStatus(uplink))
					}
				]
			}
		];
		if (hub) {
			out.push({
				id: 'hub',
				title: 'Hub',
				link: null,
				nodes: [{ id: hub.id, h: deviceHeight, view: { kind: 'device', device: hub } }],
				edges: []
			});
		}
		const peers = snapshot.devices.filter((d) => !d.self);
		out.push({
			id: 'tailnet',
			title: 'Tailnet',
			link: 'down',
			busLabel: 'tailnet',
			nodes: peers.map((d) => ({ id: d.id, h: deviceHeight, view: { kind: 'device', device: d } })),
			edges: peers.map(peerEdge)
		});
		return out;
	});

	const layout = $derived(width >= listBelow ? layoutMap(width, groups, hub?.id ?? '') : null);
	const nodes = $derived(groups.flatMap((g) => g.nodes));

	const labelClass: Record<EdgeState, string> = {
		live: 'border-line text-fg-subtle',
		warn: 'border-amber/25 text-amber-fg/90',
		down: 'border-line text-fg-faint',
		idle: 'border-line text-fg-faint'
	};

	function dashed(state: EdgeState): boolean {
		return state === 'down' || state === 'idle';
	}

	function yesNo(v: boolean): string {
		return v ? 'yes' : 'no';
	}
</script>

{#snippet card(view: View)}
	{#if view.kind === 'internet'}
		<MapCard
			icon={Globe}
			title="Internet"
			status={uplinkStatus(uplink)}
			subtitle={uplink.publicIp
				? `Public IP ${uplink.publicIp}`
				: uplink.checkedAt
					? 'The hub’s uplink'
					: 'Waiting for netcheck'}
		>
			<MapFact label="ipv4">{uplink.checkedAt ? yesNo(uplink.ipv4) : '—'}</MapFact>
			<MapFact label="ipv6">{uplink.checkedAt ? yesNo(uplink.ipv6) : '—'}</MapFact>
		</MapCard>
	{:else if view.kind === 'derp'}
		<MapCard
			icon={RadioTower}
			title="Tailscale DERP"
			status={derpStatus(uplink)}
			subtitle={uplink.derp
				? `Home relay: ${uplink.derp.name}`
				: uplink.checkedAt
					? 'No relay answered'
					: 'Waiting for netcheck'}
		>
			<MapFact label="region">{uplink.derp?.code ?? '—'}</MapFact>
			<MapFact label="udp">{uplink.checkedAt ? (uplink.udp ? 'yes' : 'blocked') : '—'}</MapFact>
		</MapCard>
	{:else}
		{@const d = view.device}
		{@const m = d.metrics}
		<MapCard
			icon={deviceIcon(d)}
			title={d.name}
			status={d.status}
			subtitle={d.online
				? d.self && m?.uptime
					? `${d.statusReason} · up ${duration(m.uptime)}`
					: d.statusReason
				: d.lastSeen
					? `Last seen ${dateTime(d.lastSeen)}`
					: d.statusReason}
			href={resolve('/devices/[id]', { id: d.id })}
			highlight={d.self}
		>
			{#if d.self && m}
				<MapFact label="cpu">{percent(m.cpu, 1)}</MapFact>
				<MapFact label="ram">{percent(ratio(m.memory?.used, m.memory?.total), 1)}</MapFact>
				<MapFact label="disk">{percent(ratio(m.disk?.used, m.disk?.total), 1)}</MapFact>
				<MapFact label="load">{m.load ? m.load[0].toFixed(2) : '—'}</MapFact>
				<MapFact label="net" wide>↓ {rateShort(m.netRx)}/s ↑ {rateShort(m.netTx)}/s</MapFact>
			{:else}
				<MapFact label="os">{osLabel(d.os)}</MapFact>
				{#if d.self}
					<MapFact label="version">{snapshot.hub.version || '—'}</MapFact>
				{:else}
					<MapFact label="rtt">{latency(d.path.rttMs)}</MapFact>
				{/if}
				{#if m}
					<MapFact label="cpu">{percent(m.cpu)}</MapFact>
					<MapFact label="disk">{percent(ratio(m.disk?.used, m.disk?.total))}</MapFact>
				{:else}
					<MapFact label="link">{d.online && !d.self ? linkLabel(d) : '—'}</MapFact>
					<MapFact label="derp">{d.path.homeDerp ?? '—'}</MapFact>
				{/if}
				<MapFact label="tailnet ip" wide>{tailnetIP(d) ?? '—'}</MapFact>
			{/if}
		</MapCard>
	{/if}
{/snippet}

<div bind:clientWidth={width} class="w-full">
	{#if layout}
		<div class="relative map-grid" style:height="{layout.height}px">
			<svg
				{width}
				height={layout.height}
				class="pointer-events-none absolute inset-0"
				aria-hidden="true"
			>
				{#each layout.paths as p (p.key)}
					<path d={p.trunk} fill="none" class="stroke-[#3a3a3a]" stroke-width="1" />
					{#if dashed(p.state)}
						<!-- Hide the shared bus under the branch, so the dashes show. -->
						<path d={p.branch} fill="none" class="stroke-surface" stroke-width="3" />
						<path
							d={p.branch}
							fill="none"
							class="stroke-fg-faint"
							stroke-width="1"
							stroke-dasharray="3 4"
						/>
					{:else}
						<path d={p.branch} fill="none" class="stroke-[#3a3a3a]" stroke-width="1" />
						<path
							d={p.trunk + p.branch.replace(/^M[^V]+/, '')}
							fill="none"
							class={[p.state === 'warn' ? 'stroke-amber' : 'stroke-green-fg', 'animate-flow']}
							stroke-opacity="0.85"
							stroke-width="1.5"
							stroke-dasharray="5 19"
							stroke-linecap="round"
						/>
					{/if}
				{/each}
				{#each layout.busLabels as b (b.key)}
					<text
						x={b.x}
						y={b.y}
						text-anchor={b.anchor}
						class="fill-fg-faint font-mono text-[10px] tracking-wide uppercase"
					>
						{b.text}
					</text>
				{/each}
			</svg>
			{#each layout.paths as p (p.key)}
				<div
					aria-hidden="true"
					class={[
						'pointer-events-none absolute -translate-x-1/2 -translate-y-1/2 rounded border bg-canvas px-1.5 py-px font-mono text-[10px] leading-4 whitespace-nowrap',
						labelClass[p.state]
					]}
					style:left="{p.lx}px"
					style:top="{p.ly}px"
				>
					{p.label}
				</div>
			{/each}
			{#each nodes as n (n.id)}
				{@const b = layout.boxes.get(n.id)}
				{#if b}
					<div
						class="absolute"
						style:left="{b.x}px"
						style:top="{b.y}px"
						style:width="{b.w}px"
						style:height="{b.h}px"
					>
						{@render card(n.view)}
					</div>
				{/if}
			{/each}
		</div>
	{:else if width > 0}
		<div class="space-y-5 px-1 py-3">
			{#each groups as g (g.id)}
				{#if g.nodes.length}
					{#if !g.link}
						{#each g.nodes as n (n.id)}
							<div style:height="{n.h}px">{@render card(n.view)}</div>
						{/each}
					{:else}
						{@const edges = new Map(g.edges.map((e) => [e.to, e]))}
						<section>
							<h3 class="mb-2 font-mono text-[10px] tracking-wider text-fg-faint uppercase">
								{g.title} · {g.nodes.length}
							</h3>
							<div class="relative space-y-3 pl-5">
								<span class="absolute top-0 bottom-6 left-[7px] w-px bg-line-heavy/60"></span>
								{#each g.nodes as n (n.id)}
									{@const e = edges.get(n.id)}
									<div class="relative">
										<span
											class={[
												'absolute top-[30px] -left-[13px] w-[13px]',
												e && dashed(e.state)
													? 'border-t border-dashed border-fg-faint'
													: 'h-px bg-line-heavy/60'
											]}
										></span>
										{#if e}
											<p
												aria-hidden="true"
												class={[
													'mb-1 font-mono text-[10px]',
													e.state === 'warn'
														? 'text-amber-fg'
														: dashed(e.state)
															? 'text-fg-faint'
															: 'text-fg-subtle'
												]}
											>
												{e.label}
											</p>
										{/if}
										<div style:height="{n.h}px">{@render card(n.view)}</div>
									</div>
								{/each}
							</div>
						</section>
					{/if}
				{/if}
			{/each}
		</div>
	{:else}
		<div class="h-130"></div>
	{/if}
</div>
