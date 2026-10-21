<!-- Whether the stream from the hub is live, and when the latest data came. -->
<script lang="ts">
	import { clock } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	const labels = { live: 'Live telemetry', connecting: 'Connecting', reconnecting: 'Reconnecting' };
	const isLive = $derived(live.connection === 'live');
</script>

<div class="hidden items-center gap-2 font-mono text-[11.5px] text-fg-subtle md:flex" role="status">
	<StatusDot
		status={isLive ? 'online' : live.connection === 'connecting' ? 'unknown' : 'attention'}
		pulse={isLive}
	/>
	<span class={isLive ? 'text-fg-muted' : 'text-amber-fg'}>{labels[live.connection]}</span>
	{#if live.receivedAt > 0}
		<span class="text-fg-faint">Updated {clock(live.receivedAt)}</span>
	{/if}
</div>
