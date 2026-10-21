<!-- A small dot in the color of a status. It's decoration: say the status in words too. -->
<script lang="ts">
	import { statusMeta, type Status } from '#lib/status.js';

	interface Props {
		status: Status;
		/** Pulses while online, for things that are live. */
		pulse?: boolean;
		class?: string;
	}

	let { status, pulse = false, class: className }: Props = $props();

	const dot = $derived(statusMeta[status].dot);
</script>

<span class={['relative inline-flex size-2 shrink-0', className]} aria-hidden="true">
	{#if pulse && status === 'online'}
		<span class={['absolute inline-flex size-full animate-ping-slow rounded-full opacity-60', dot]}
		></span>
	{/if}
	<span class={['relative inline-flex size-2 rounded-full', dot]}></span>
</span>
