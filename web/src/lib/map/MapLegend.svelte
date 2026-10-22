<!-- What the network map's colors and dashed lines mean. -->
<script lang="ts">
	import { statusMeta, type Status } from '#lib/status.js';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	const statuses: Status[] = ['online', 'attention', 'offline', 'unknown'];
</script>

<div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-fg-subtle">
	{#each statuses as s (s)}
		<span class="inline-flex items-center gap-1.5">
			<StatusDot status={s} />
			{statusMeta[s].label}
		</span>
	{/each}
	<span class="inline-flex items-center gap-1.5">
		<svg width="16" height="2" aria-hidden="true">
			<line x1="0" x2="16" y1="1" y2="1" class="stroke-fg-faint" stroke-dasharray="3 3" />
		</svg>
		Link not observed
	</span>
</div>
