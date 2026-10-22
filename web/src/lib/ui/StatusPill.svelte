<!-- A status in capitals, such as LIVE or OFFLINE, in the status's color. -->
<script lang="ts">
	import { statusMeta, type Status } from '#lib/status.js';
	import Badge from './Badge.svelte';

	interface Props {
		status: Status;
		/** Text in place of the status's own, such as STOPPED. */
		label?: string;
	}

	let { status, label }: Props = $props();

	const meta = $derived(statusMeta[status]);
</script>

<Badge tone={meta.tone} mono>{label ?? meta.badge}</Badge>
