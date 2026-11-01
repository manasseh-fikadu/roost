<!-- Whether the stream from the hub is live, and when the latest data came. -->
<script lang="ts">
	import { clock } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	const labels = { live: 'Live telemetry', connecting: 'Connecting', reconnecting: 'Reconnecting' };
	const isLive = $derived(live.connection === 'live');
</script>

<!-- On a phone only the dot shows, with the state as its tooltip. -->
<div
	class="flex items-center gap-2 font-mono text-[11.5px] text-fg-subtle"
	title={labels[live.connection]}
>
	<StatusDot
		status={isLive ? 'online' : live.connection === 'connecting' ? 'unknown' : 'attention'}
		pulse={isLive}
	/>
	<!-- Only the state is announced: the time next to it changes with every snapshot. -->
	<span class={['hidden md:inline', isLive ? 'text-fg-muted' : 'text-amber-fg']} role="status"
		>{labels[live.connection]}</span
	>
	{#if live.receivedAt > 0}
		<span class="hidden text-fg-faint md:inline">Updated {clock(live.receivedAt)}</span>
	{/if}
</div>
