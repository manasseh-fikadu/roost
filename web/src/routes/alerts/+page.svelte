<script lang="ts">
	import Bell from '@lucide/svelte/icons/bell';
	import Placeholder from '#lib/shell/Placeholder.svelte';
</script>

<Placeholder
	title="Alerts"
	description="What needs your attention, and what did."
	icon={Bell}
	issue={26}
>
	Alerts open when a device goes offline, a disk fills, a check fails or a container crashes, and
	close when it's fixed. Acknowledge, snooze or add notes here.
</Placeholder>
