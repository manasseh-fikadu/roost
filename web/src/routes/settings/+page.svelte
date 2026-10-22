<script lang="ts">
	import Settings from '@lucide/svelte/icons/settings';
	import Placeholder from '#lib/shell/Placeholder.svelte';
</script>

<Placeholder
	title="Settings"
	description="How Roost behaves, and who can use it."
	icon={Settings}
	issue={10}
>
	Roost's name, alert thresholds, watched systemd units, retention, and who's an admin or a viewer.
	Until then, <code class="font-mono text-fg-muted">roost admin</code> manages access.
</Placeholder>
