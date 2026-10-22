<script lang="ts">
	import HeartPulse from '@lucide/svelte/icons/heart-pulse';
	import Placeholder from '#lib/shell/Placeholder.svelte';
</script>

<Placeholder
	title="Checks"
	description="Probes of your services, and the health score they add up to."
	icon={HeartPulse}
	issue={25}
>
	HTTP, TCP, ping and DNS checks, each with its uptime and latency history.
</Placeholder>
